// Package ops implements the user-level operations shared by the CLI and the UI.
package ops

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"siq_converter/internal/archive"
	"siq_converter/internal/scaffold"
	"siq_converter/internal/siqxml"
	"siq_converter/internal/siqyaml"
)

// ErrExists is returned when the output already exists and force is not set.
var ErrExists = errors.New("output already exists")

// Result describes a finished unpack or pack.
type Result struct {
	Out      string
	Files    int
	Warnings []string
}

// UnpackTarget returns the default output folder for a .siq file.
func UnpackTarget(in string) string { return strings.TrimSuffix(in, filepath.Ext(in)) }

// PackTarget returns the default output .siq file for a package folder: next
// to the folder, named after it. "." and other relative paths are resolved
// first, so `pack .` gives "<folder name>.siq" rather than "..siq".
func PackTarget(in string) string {
	dir := packageDir(in)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return filepath.Join(filepath.Dir(dir), filepath.Base(dir)+".siq")
}

// packageDir accepts a package folder or its content.yaml.
func packageDir(in string) string {
	in = filepath.Clean(in)
	if filepath.Base(in) == archive.ContentYAML {
		return filepath.Dir(in)
	}
	return in
}

// Unpack converts a .siq into a folder with content.yaml and media.
// With force, an existing unpacked package in the output folder is replaced.
// The output folder itself is kept (so "." works and open windows stay valid);
// only its contents are swapped, after the new contents are fully written.
func Unpack(in, out string, force bool) (*Result, error) {
	if out == "" {
		out = UnpackTarget(in)
	}
	b, err := archive.ReadSIQ(in)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(out); err == nil && !info.IsDir() {
		return nil, fmt.Errorf("%s: exists and is not a folder; choose another output folder", out)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	replace := false
	entries, err := os.ReadDir(out)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		// Without a listing we cannot tell whether the folder is empty, so
		// never write into it (that could silently overwrite its files).
		return nil, fmt.Errorf("%s: cannot list the output folder: %w", out, err)
	}
	if len(entries) > 0 {
		if !force {
			return nil, fmt.Errorf("%s: %w (folder is not empty)", out, ErrExists)
		}
		// Overwriting deletes the folder's contents, so only ever replace an
		// unpacked package, and never the folder holding the archive itself.
		if inside, err := insideDir(in, out); err != nil || inside {
			return nil, fmt.Errorf("%s: refusing to delete the folder that contains %s; choose another output folder", out, in)
		}
		if _, err := os.Stat(filepath.Join(out, archive.ContentYAML)); err != nil {
			return nil, fmt.Errorf("%s: refusing to delete a folder that is not an unpacked package (it has no %s); choose an empty or new folder", out, archive.ContentYAML)
		}
		replace = true
	}

	// Write the new contents next to the output folder first, so a failure
	// (e.g. a full disk) leaves the existing package untouched.
	absOut, err := filepath.Abs(out)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(absOut)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	staged, err := os.MkdirTemp(parent, "."+filepath.Base(absOut)+".unpacking-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staged)
	if err := archive.WriteDir(staged, b); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absOut, 0o755); err != nil {
		return nil, err
	}
	if err := swapContents(absOut, staged, replace); err != nil {
		return nil, err
	}
	return &Result{Out: out, Files: len(b.Files)}, nil
}

// swapContents moves the entries of staged into dir. With replace, dir's
// current entries are first moved to a backup folder, restored if anything
// fails, and deleted at the end.
func swapContents(dir, staged string, replace bool) error {
	backup := ""
	if replace {
		backup = staged + ".previous"
		if err := os.Mkdir(backup, 0o755); err != nil {
			return err
		}
		if moved, err := moveEntries(dir, backup); err != nil {
			if _, rerr := moveNames(backup, dir, moved); rerr != nil {
				return fmt.Errorf("%w; restoring the previous package also failed (%v): its files are in %s", err, rerr, backup)
			}
			os.Remove(backup)
			return err
		}
	}
	if moved, err := moveEntries(staged, dir); err != nil {
		for _, name := range moved {
			os.RemoveAll(filepath.Join(dir, name))
		}
		if backup == "" {
			return err
		}
		if _, rerr := moveEntries(backup, dir); rerr != nil {
			return fmt.Errorf("%w; restoring the previous package also failed (%v): its files are in %s", err, rerr, backup)
		}
		os.Remove(backup)
		return err
	}
	if backup != "" {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("unpacked, but could not remove the previous package at %s: %w", backup, err)
		}
	}
	return nil
}

// moveEntries renames every entry of from into to, returning the names moved
// so far (also on error).
func moveEntries(from, to string) ([]string, error) {
	entries, err := os.ReadDir(from)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return moveNames(from, to, names)
}

// rename is os.Rename; tests replace it to simulate failures.
var rename = os.Rename

func moveNames(from, to string, names []string) ([]string, error) {
	var moved []string
	for _, name := range names {
		if err := rename(filepath.Join(from, name), filepath.Join(to, name)); err != nil {
			return moved, err
		}
		moved = append(moved, name)
	}
	return moved, nil
}

// insideDir reports whether path is dir or lies inside it. It compares
// filesystem identities rather than strings, so symlinks and differently
// cased names on case-insensitive filesystems are seen through.
func insideDir(path, dir string) (bool, error) {
	dirInfo, err := os.Stat(dir)
	if err != nil {
		return false, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	for p := abs; ; p = filepath.Dir(p) {
		if info, err := os.Stat(p); err == nil && os.SameFile(info, dirInfo) {
			return true, nil
		}
		if filepath.Dir(p) == p {
			return false, nil
		}
	}
}

// Pack converts a package folder into a .siq. A missing package id is
// generated; files the package references must exist.
func Pack(in, out string, force bool) (*Result, error) {
	in = packageDir(in)
	if out == "" {
		out = PackTarget(in)
	}
	if !force {
		if _, err := os.Stat(out); err == nil {
			return nil, fmt.Errorf("%s: %w", out, ErrExists)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	b, err := archive.ReadDir(in)
	if err != nil {
		return nil, err
	}
	skipped := excludeArchives(b, out)
	warnings, err := archive.Check(b)
	if err != nil {
		return nil, err
	}
	warnings = append(skipped, warnings...)
	if b.Package.ID == "" {
		b.Package.ID = newUUID()
	}
	if err := archive.WriteSIQ(out, b); err != nil {
		return nil, err
	}
	return &Result{Out: out, Files: len(b.Files), Warnings: warnings}, nil
}

// excludeArchives drops .siq files from the package folder: the output itself
// (when it is written inside the folder) and older archives, which would
// otherwise be packed into the new archive on every repack.
func excludeArchives(b *archive.Bundle, out string) (warnings []string) {
	outInfo, outErr := os.Stat(out)
	var kept []archive.File
	for _, f := range b.Files {
		if !strings.EqualFold(filepath.Ext(f.Path), ".siq") {
			kept = append(kept, f)
			continue
		}
		if info, err := os.Stat(f.Src); outErr == nil && err == nil && os.SameFile(info, outInfo) {
			continue // the archive being written
		}
		warnings = append(warnings, "skipped "+f.Path+": .siq archives inside the package folder are not packed")
	}
	b.Files = kept
	return warnings
}

// Init creates a new package folder with example questions of every type.
func Init(dir string) error { return scaffold.Init(dir) }

// Verify runs the full round trip .siq -> folder -> .siq in a temp directory
// and compares content and files. It returns a one-line summary on success.
func Verify(in string) (string, error) {
	orig, err := archive.ReadSIQ(in)
	if err != nil {
		return "", err
	}
	origXML, err := archive.ReadContentXML(in)
	if err != nil {
		return "", err
	}
	var problems []string
	fail := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	// YAML writer is idempotent and the model survives YAML.
	y1, err := siqyaml.Encode(orig.Package)
	if err != nil {
		return "", err
	}
	p2, err := siqyaml.Decode(y1)
	if err != nil {
		return "", fmt.Errorf("re-reading generated YAML: %w", err)
	}
	if !reflect.DeepEqual(orig.Package, p2) {
		fail("package model changed after YAML round trip")
	}
	if y2, _ := siqyaml.Encode(p2); !bytes.Equal(y1, y2) {
		fail("YAML output is not stable")
	}

	// Full trip through the file system.
	tmp, err := os.MkdirTemp("", "siqconv-verify-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	dir := filepath.Join(tmp, "pkg")
	if err := archive.WriteDir(dir, orig); err != nil {
		return "", err
	}
	fromDir, err := archive.ReadDir(dir)
	if err != nil {
		return "", err
	}
	if _, err := archive.Check(fromDir); err != nil {
		fail("%v", err)
	}
	outSIQ := filepath.Join(tmp, "out.siq")
	if err := archive.WriteSIQ(outSIQ, fromDir); err != nil {
		return "", err
	}
	back, err := archive.ReadSIQ(outSIQ)
	if err != nil {
		return "", fmt.Errorf("re-reading generated .siq: %w", err)
	}
	if !reflect.DeepEqual(orig.Package, back.Package) {
		fail("package model changed after .siq round trip")
	}

	// Model-independent comparison of the XML.
	newXML, err := archive.ReadContentXML(outSIQ)
	if err != nil {
		return "", err
	}
	c1, err := siqxml.Canonical(origXML)
	if err != nil {
		return "", err
	}
	c2, err := siqxml.Canonical(newXML)
	if err != nil {
		return "", err
	}
	if c1 != c2 {
		fail("content.xml differs:\n%s", firstDiff(c1, c2))
	}

	// Files must be byte-identical.
	want := map[string]string{}
	for _, f := range orig.Files {
		if want[f.Path], err = f.Hash(); err != nil {
			return "", err
		}
	}
	got := map[string]string{}
	for _, f := range back.Files {
		if got[f.Path], err = f.Hash(); err != nil {
			return "", err
		}
	}
	for p, h := range want {
		if got[p] == "" {
			fail("file lost: %s", p)
		} else if got[p] != h {
			fail("file changed: %s", p)
		}
	}
	for p := range got {
		if want[p] == "" {
			fail("unexpected file: %s", p)
		}
	}

	if len(problems) > 0 {
		return "", fmt.Errorf("verify failed:\n- %s", strings.Join(problems, "\n- "))
	}
	return fmt.Sprintf("OK: %s round-trips losslessly (%d rounds, %d files)", in, len(orig.Package.Rounds), len(orig.Files)), nil
}

func firstDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) || i < len(bl); i++ {
		var x, y string
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x != y {
			return fmt.Sprintf("  line %d\n  original:  %s\n  converted: %s", i+1, x, y)
		}
	}
	return ""
}

func newUUID() string {
	var u [16]byte
	_, _ = rand.Read(u[:])
	u[6] = u[6]&0x0f | 0x40
	u[8] = u[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}
