package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"siq_converter/internal/archive"
)

// minimalSIQ packs testdata/minimal into dir and returns the .siq path.
func minimalSIQ(t *testing.T, dir string) string {
	t.Helper()
	b, err := archive.ReadDir("../../testdata/minimal")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "min.siq")
	if err := archive.WriteSIQ(p, b); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUnpackForceOnlyDeletesUnpackedPackages(t *testing.T) {
	tmp := t.TempDir()
	siq := minimalSIQ(t, tmp)

	// A non-empty folder that is not a package is never deleted.
	docs := filepath.Join(tmp, "Documents")
	os.MkdirAll(docs, 0o755)
	os.WriteFile(filepath.Join(docs, "thesis.txt"), []byte("precious"), 0o644)
	if _, err := Unpack(siq, docs, false); !errors.Is(err, ErrExists) {
		t.Errorf("without force: %v", err)
	}
	if _, err := Unpack(siq, docs, true); err == nil || !strings.Contains(err.Error(), "not an unpacked package") {
		t.Errorf("force into a non-package folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(docs, "thesis.txt")); err != nil {
		t.Fatal("unrelated file was deleted")
	}

	// The folder holding the archive is never deleted, even if it looks like a package.
	os.WriteFile(filepath.Join(tmp, archive.ContentYAML), []byte("name: x\n"), 0o644)
	if _, err := Unpack(siq, tmp, true); err == nil || !strings.Contains(err.Error(), "contains") {
		t.Errorf("force into the folder holding the archive: %v", err)
	}
	if _, err := os.Stat(siq); err != nil {
		t.Fatal("input archive was deleted")
	}

	// Re-unpacking over a previously unpacked package works.
	out := filepath.Join(tmp, "unpacked")
	if _, err := Unpack(siq, out, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Unpack(siq, out, true); err != nil {
		t.Errorf("force over an unpacked package: %v", err)
	}
}

func TestPackTargetResolvesRelativePaths(t *testing.T) {
	tmp := t.TempDir()
	pkg := filepath.Join(tmp, "quiz")
	os.MkdirAll(pkg, 0o755)
	t.Chdir(pkg)
	tmpReal, err := filepath.EvalSymlinks(tmp) // macOS temp dirs live behind a symlink
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{".", "./", archive.ContentYAML, pkg, filepath.Join(pkg, archive.ContentYAML)} {
		got := PackTarget(in)
		dir, err := filepath.EvalSymlinks(filepath.Dir(got))
		if err != nil || dir != tmpReal || filepath.Base(got) != "quiz.siq" {
			t.Errorf("PackTarget(%q) = %q, want %s", in, got, filepath.Join(tmp, "quiz.siq"))
		}
	}
}
