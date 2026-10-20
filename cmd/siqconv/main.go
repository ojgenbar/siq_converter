// Command siqconv converts SIGame .siq packages to editable YAML folders and back.
package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"siq_converter/internal/archive"
	"siq_converter/internal/siqxml"
	"siq_converter/internal/siqyaml"
)

const usage = `siqconv converts SIGame question packages (.siq) to YAML folders and back.

Usage:
  siqconv unpack [-f] <in.siq> [outdir]   .siq -> folder with content.yaml and media
  siqconv pack   [-f] <dir> [out.siq]     folder -> .siq
  siqconv convert [-f] <path> [out]       unpack a .siq, or pack a folder / content.yaml
  siqconv verify <in.siq>                 check that .siq -> YAML -> .siq loses nothing

  -f  overwrite the output (.siq file, or remove a non-empty output folder)
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("no command given")
	}
	cmd, rest := args[0], args[1:]
	fset := flag.NewFlagSet(cmd, flag.ContinueOnError)
	force := fset.Bool("f", false, "overwrite output")
	fset.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	if err := fset.Parse(rest); err != nil {
		return err
	}
	pos := fset.Args()
	if len(pos) < 1 || len(pos) > 2 {
		fset.Usage()
		return fmt.Errorf("%s: expected 1 or 2 arguments", cmd)
	}
	in, out := pos[0], ""
	if len(pos) == 2 {
		out = pos[1]
	}
	switch cmd {
	case "unpack":
		return unpack(in, out, *force)
	case "pack":
		return pack(in, out, *force)
	case "convert":
		if strings.EqualFold(filepath.Ext(in), ".siq") {
			return unpack(in, out, *force)
		}
		if filepath.Base(in) == archive.ContentYAML {
			in = filepath.Dir(in)
		}
		return pack(in, out, *force)
	case "verify":
		return verify(in)
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	fmt.Fprint(os.Stderr, usage)
	return fmt.Errorf("unknown command %q", cmd)
}

func unpack(in, out string, force bool) error {
	if out == "" {
		out = strings.TrimSuffix(in, filepath.Ext(in))
	}
	b, err := archive.ReadSIQ(in)
	if err != nil {
		return err
	}
	if force {
		if err := os.RemoveAll(out); err != nil {
			return err
		}
	}
	if err := archive.WriteDir(out, b); err != nil {
		return err
	}
	fmt.Printf("unpacked %s -> %s (%d files)\n", in, out, len(b.Files))
	return nil
}

func pack(in, out string, force bool) error {
	in = filepath.Clean(in)
	if out == "" {
		out = in + ".siq"
	}
	if !force {
		if _, err := os.Stat(out); err == nil {
			return fmt.Errorf("%s already exists (use -f to overwrite)", out)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	b, err := archive.ReadDir(in)
	if err != nil {
		return err
	}
	warnings, err := archive.Check(b)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if b.Package.ID == "" {
		b.Package.ID = newUUID()
	}
	if err := archive.WriteSIQ(out, b); err != nil {
		return err
	}
	fmt.Printf("packed %s -> %s (%d files)\n", in, out, len(b.Files))
	return nil
}

// verify runs the full round trip .siq -> folder -> .siq in a temp directory
// and compares content and files.
func verify(in string) error {
	orig, err := archive.ReadSIQ(in)
	if err != nil {
		return err
	}
	origXML, err := archive.ReadContentXML(in)
	if err != nil {
		return err
	}
	var problems []string
	fail := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	// YAML writer is idempotent and the model survives YAML.
	y1, err := siqyaml.Encode(orig.Package)
	if err != nil {
		return err
	}
	p2, err := siqyaml.Decode(y1)
	if err != nil {
		return fmt.Errorf("re-reading generated YAML: %w", err)
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
		return err
	}
	defer os.RemoveAll(tmp)
	dir := filepath.Join(tmp, "pkg")
	if err := archive.WriteDir(dir, orig); err != nil {
		return err
	}
	fromDir, err := archive.ReadDir(dir)
	if err != nil {
		return err
	}
	if _, err := archive.Check(fromDir); err != nil {
		fail("%v", err)
	}
	outSIQ := filepath.Join(tmp, "out.siq")
	if err := archive.WriteSIQ(outSIQ, fromDir); err != nil {
		return err
	}
	back, err := archive.ReadSIQ(outSIQ)
	if err != nil {
		return fmt.Errorf("re-reading generated .siq: %w", err)
	}
	if !reflect.DeepEqual(orig.Package, back.Package) {
		fail("package model changed after .siq round trip")
	}

	// Model-independent comparison of the XML.
	newXML, err := archive.ReadContentXML(outSIQ)
	if err != nil {
		return err
	}
	c1, err := siqxml.Canonical(origXML)
	if err != nil {
		return err
	}
	c2, err := siqxml.Canonical(newXML)
	if err != nil {
		return err
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
		return fmt.Errorf("verify failed:\n- %s", strings.Join(problems, "\n- "))
	}
	fmt.Printf("OK: %s round-trips losslessly (%d rounds, %d files)\n", in, len(orig.Package.Rounds), len(orig.Files))
	return nil
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
