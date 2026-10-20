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

func TestUnpackGuardIgnoresCase(t *testing.T) {
	tmp := t.TempDir()
	real := filepath.Join(tmp, "Real")
	os.MkdirAll(real, 0o755)
	siq := minimalSIQ(t, real)
	os.WriteFile(filepath.Join(real, archive.ContentYAML), []byte("name: x\n"), 0o644)
	lower := filepath.Join(tmp, "real")
	if _, err := os.Stat(lower); err != nil {
		t.Skip("case-sensitive filesystem")
	}
	if _, err := Unpack(siq, lower, true); err == nil {
		t.Error("expected the guard to refuse the differently cased folder holding the archive")
	}
	if _, err := os.Stat(siq); err != nil {
		t.Fatal("source archive was deleted")
	}
}

func TestUnpackIntoCurrentFolder(t *testing.T) {
	tmp := t.TempDir()
	siq := minimalSIQ(t, tmp)
	dir := filepath.Join(tmp, "here")
	os.MkdirAll(dir, 0o755)
	before, _ := os.Stat(dir)
	t.Chdir(dir)
	if _, err := Unpack(siq, ".", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Unpack(siq, ".", true); err != nil {
		t.Fatalf("forced unpack into .: %v", err)
	}
	after, err := os.Stat(dir)
	if err != nil || !os.SameFile(before, after) {
		t.Error("the output folder itself was replaced; open terminals would lose it")
	}
	if _, err := os.Stat(filepath.Join(dir, archive.ContentYAML)); err != nil {
		t.Error("content.yaml missing after unpacking into .")
	}
	if entries, _ := os.ReadDir(tmp); len(entries) != 2 {
		t.Errorf("staging folders left behind: %v", entries)
	}
}

func TestPackThroughSymlinkedFolder(t *testing.T) {
	tmp := t.TempDir()
	siq := minimalSIQ(t, tmp)
	real := filepath.Join(tmp, "real")
	if _, err := Unpack(siq, real, false); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(tmp, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skip("symlinks not supported:", err)
	}
	res, err := Pack(alias, filepath.Join(tmp, "out.siq"), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 2 {
		t.Errorf("packed %d files through the symlink, want 2", res.Files)
	}
}

func TestFailedRollbackReportsBackup(t *testing.T) {
	tmp := t.TempDir()
	dir, staged := filepath.Join(tmp, "pkg"), filepath.Join(tmp, ".pkg.unpacking-1")
	os.MkdirAll(dir, 0o755)
	os.MkdirAll(staged, 0o755)
	os.WriteFile(filepath.Join(dir, archive.ContentYAML), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(staged, archive.ContentYAML), []byte("new"), 0o644)

	// Moving the old package out works; moving the new one in and restoring both fail.
	calls := 0
	rename = func(from, to string) error {
		calls++
		if calls == 1 {
			return os.Rename(from, to)
		}
		return errors.New("disk on fire")
	}
	defer func() { rename = os.Rename }()

	err := swapContents(dir, staged, true)
	backup := staged + ".previous"
	if err == nil || !strings.Contains(err.Error(), "restoring the previous package also failed") || !strings.Contains(err.Error(), backup) {
		t.Fatalf("error should report both failures and the backup location, got: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(backup, archive.ContentYAML)); string(data) != "old" {
		t.Error("previous package is not in the reported backup folder")
	}
}
