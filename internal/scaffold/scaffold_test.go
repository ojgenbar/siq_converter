package scaffold

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"siq_converter/internal/archive"
)

func TestInitCoversAllTypesAndPacks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "My: quiz")
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	if err := Init(dir); err == nil {
		t.Error("Init into a non-empty folder must fail")
	}

	b, err := archive.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b.Package.Name != "My: quiz" || b.Package.ID != "" {
		t.Errorf("name = %q, id = %q", b.Package.Name, b.Package.ID)
	}

	types := map[string]bool{}
	answerTypes := map[string]bool{}
	for _, r := range b.Package.Rounds {
		for _, th := range r.Themes {
			for _, q := range th.Questions {
				typ := q.Type
				if typ == "" {
					typ = "simple"
				}
				types[typ] = true
				for _, p := range q.Params {
					if p.Name == "answerType" {
						answerTypes[p.Value] = true
					}
				}
			}
		}
	}
	for _, want := range []string{"simple", "stake", "stakeAll", "secret", "secretPublicPrice", "secretNoQuestion", "noRisk", "forAll"} {
		if !types[want] {
			t.Errorf("no %s question", want)
		}
	}
	for _, want := range []string{"select", "number", "point"} {
		if !answerTypes[want] {
			t.Errorf("no %s answer type", want)
		}
	}

	// Every referenced media file is missing, and the header comment lists exactly those.
	_, err = archive.Check(b)
	if err == nil {
		t.Fatal("expected missing files")
	}
	var missing []string
	for _, line := range strings.Split(err.Error(), "\n")[1:] {
		missing = append(missing, strings.Fields(line)[0])
	}
	sort.Strings(missing)
	want := []string{"Audio/song.mp3", "Html/page.html", "Images/answer.jpg", "Images/logo.png", "Images/map.png", "Images/question.jpg", "Video/clip.mp4", "Video/story_part1.mp4", "Video/story_part2.mp4"}
	if !reflect.DeepEqual(missing, want) {
		t.Errorf("missing = %v, want %v", missing, want)
	}
	header := strings.SplitN(template, "\nsiq:", 2)[0]
	for _, f := range want {
		if !strings.Contains(header, f) {
			t.Errorf("header comment does not mention %s", f)
		}
	}

	// Once the files are added, it packs and reads back unchanged.
	for _, f := range want {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err = archive.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if warnings, err := archive.Check(b); err != nil || len(warnings) > 0 {
		t.Fatalf("check: %v %v", err, warnings)
	}
	out := filepath.Join(t.TempDir(), "out.siq")
	if err := archive.WriteSIQ(out, b); err != nil {
		t.Fatal(err)
	}
	back, err := archive.ReadSIQ(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Package, b.Package) {
		t.Error("package changed after pack")
	}
}

func TestInitDotIsNamedAfterFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Quiz night")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := Init("."); err != nil {
		t.Fatal(err)
	}
	b, err := archive.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	if b.Package.Name != "Quiz night" {
		t.Errorf("name = %q", b.Package.Name)
	}
}
