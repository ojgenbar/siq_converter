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

// PackTarget returns the default output .siq file for a package folder.
func PackTarget(in string) string { return filepath.Clean(in) + ".siq" }

// Unpack converts a .siq into a folder with content.yaml and media.
// With force, an existing output folder is removed first.
func Unpack(in, out string, force bool) (*Result, error) {
	if out == "" {
		out = UnpackTarget(in)
	}
	b, err := archive.ReadSIQ(in)
	if err != nil {
		return nil, err
	}
	if entries, err := os.ReadDir(out); err == nil && len(entries) > 0 {
		if !force {
			return nil, fmt.Errorf("%s: %w (folder is not empty)", out, ErrExists)
		}
		if err := os.RemoveAll(out); err != nil {
			return nil, err
		}
	}
	if err := archive.WriteDir(out, b); err != nil {
		return nil, err
	}
	return &Result{Out: out, Files: len(b.Files)}, nil
}

// Pack converts a package folder into a .siq. A missing package id is
// generated; files the package references must exist.
func Pack(in, out string, force bool) (*Result, error) {
	in = filepath.Clean(in)
	if filepath.Base(in) == archive.ContentYAML {
		in = filepath.Dir(in)
	}
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
	warnings, err := archive.Check(b)
	if err != nil {
		return nil, err
	}
	if b.Package.ID == "" {
		b.Package.ID = newUUID()
	}
	if err := archive.WriteSIQ(out, b); err != nil {
		return nil, err
	}
	return &Result{Out: out, Files: len(b.Files), Warnings: warnings}, nil
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
		want[f.Path] = archive.Hash(f.Data)
	}
	got := map[string]string{}
	for _, f := range back.Files {
		got[f.Path] = archive.Hash(f.Data)
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
