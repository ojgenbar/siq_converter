// Command siqconv converts SIGame .siq packages to editable YAML folders and back.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"siq_converter/internal/archive"
	"siq_converter/internal/ops"
)

const usage = `siqconv converts SIGame question packages (.siq) to YAML folders and back.

Usage:
  siqconv init <dir>                      new package folder with example questions of every type
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
	case "init":
		if out != "" {
			return errors.New("init: expected 1 argument")
		}
		if err := ops.Init(in); err != nil {
			return err
		}
		fmt.Printf("created %s: edit %s, add the media files it lists, then run: siqconv pack %s\n", in, filepath.Join(in, archive.ContentYAML), in)
		return nil
	case "unpack":
		return unpack(in, out, *force)
	case "pack":
		return pack(in, out, *force)
	case "convert":
		if strings.EqualFold(filepath.Ext(in), ".siq") {
			return unpack(in, out, *force)
		}
		return pack(in, out, *force)
	case "verify":
		summary, err := ops.Verify(in)
		if err != nil {
			return err
		}
		fmt.Println(summary)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	fmt.Fprint(os.Stderr, usage)
	return fmt.Errorf("unknown command %q", cmd)
}

func unpack(in, out string, force bool) error {
	res, err := ops.Unpack(in, out, force)
	if err != nil {
		return withForceHint(err)
	}
	fmt.Printf("unpacked %s -> %s (%d files)\n", in, res.Out, res.Files)
	return nil
}

func pack(in, out string, force bool) error {
	res, err := ops.Pack(in, out, force)
	if err != nil {
		return withForceHint(err)
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	fmt.Printf("packed %s -> %s (%d files)\n", in, res.Out, res.Files)
	return nil
}

func withForceHint(err error) error {
	if errors.Is(err, ops.ErrExists) {
		return fmt.Errorf("%w (use -f to overwrite)", err)
	}
	return err
}
