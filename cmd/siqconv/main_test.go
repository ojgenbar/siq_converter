package main

import (
	"flag"
	"reflect"
	"testing"
)

func TestParseArgsAcceptsTrailingFlags(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		pos   []string
		force bool
	}{
		{[]string{"pkg", "-f"}, []string{"pkg"}, true},
		{[]string{"-f", "pkg", "out.siq"}, []string{"pkg", "out.siq"}, true},
		{[]string{"pkg", "-f", "out.siq"}, []string{"pkg", "out.siq"}, true},
		{[]string{"pkg", "out.siq"}, []string{"pkg", "out.siq"}, false},
		{[]string{"pkg", "--", "-f"}, []string{"pkg", "-f"}, false},
	} {
		fset := flag.NewFlagSet("t", flag.ContinueOnError)
		force := fset.Bool("f", false, "")
		pos, err := parseArgs(fset, tc.args)
		if err != nil || !reflect.DeepEqual(pos, tc.pos) || *force != tc.force {
			t.Errorf("%v: pos=%v force=%v err=%v", tc.args, pos, *force, err)
		}
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}, {"pack", "-h"}} {
		if err := run(args); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
}
