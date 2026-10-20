package ops

import (
	"archive/zip"
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

func TestRepackInsideFolderDoesNotNestArchives(t *testing.T) {
	tmp := t.TempDir()
	siq := minimalSIQ(t, tmp)
	pkg := filepath.Join(tmp, "pkg")
	if _, err := Unpack(siq, pkg, false); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(pkg, "old.siq"), []byte("an older build"), 0o644)
	out := filepath.Join(pkg, "build.siq")
	for i := 0; i < 3; i++ {
		res, err := Pack(pkg, out, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "old.siq") {
			t.Errorf("warnings = %v", res.Warnings)
		}
		zr, err := zip.OpenReader(out)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range zr.File {
			if strings.HasSuffix(f.Name, ".siq") {
				t.Errorf("pack %d: archive contains %s", i+1, f.Name)
			}
		}
		zr.Close()
	}
}

func TestForcedUnpackKeepsOldFolderOnFailure(t *testing.T) {
	tmp := t.TempDir()
	siq := minimalSIQ(t, tmp)
	parent := filepath.Join(tmp, "parent")
	out := filepath.Join(parent, "pkg")
	if _, err := Unpack(siq, out, false); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(out, "notes.txt"), []byte("my edits"), 0o644)
	os.Chmod(parent, 0o555) // nothing new can be created next to the folder
	defer os.Chmod(parent, 0o755)
	if _, err := Unpack(siq, out, true); err == nil {
		t.Skip("parent folder is still writable (running as root?)")
	}
	if data, err := os.ReadFile(filepath.Join(out, "notes.txt")); err != nil || string(data) != "my edits" {
		t.Error("the existing folder was lost after a failed unpack")
	}
	os.Chmod(parent, 0o755)
	if _, err := Unpack(siq, out, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "notes.txt")); err == nil {
		t.Error("forced unpack did not replace the folder")
	}
	if entries, _ := os.ReadDir(parent); len(entries) != 1 {
		t.Errorf("temporary folders left behind: %v", entries)
	}
}

func TestUnpackRefusesFileTarget(t *testing.T) {
	tmp := t.TempDir()
	siq := minimalSIQ(t, tmp)
	notes := filepath.Join(tmp, "notes")
	os.WriteFile(notes, []byte("keep me"), 0o644)
	for _, force := range []bool{false, true} {
		if _, err := Unpack(siq, notes, force); err == nil || !strings.Contains(err.Error(), "not a folder") {
			t.Errorf("force=%v: %v", force, err)
		}
	}
	if data, err := os.ReadFile(notes); err != nil || string(data) != "keep me" {
		t.Error("existing file was deleted")
	}
}

func TestUnpackGuardSeesThroughSymlinks(t *testing.T) {
	tmp := t.TempDir()
	real := filepath.Join(tmp, "real")
	os.MkdirAll(real, 0o755)
	siq := minimalSIQ(t, real)
	os.WriteFile(filepath.Join(real, archive.ContentYAML), []byte("name: x\n"), 0o644)
	alias := filepath.Join(tmp, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skip("symlinks not supported:", err)
	}
	if _, err := Unpack(filepath.Join(alias, filepath.Base(siq)), real, true); err == nil {
		t.Error("expected the guard to refuse replacing the folder holding the archive")
	}
	if _, err := os.Stat(siq); err != nil {
		t.Fatal("source archive was deleted through a symlink")
	}
}
