package archive

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"siq_converter/internal/siqxml"
)

func TestNameEncoding(t *testing.T) {
	// Entry names as they appear in a real SIGame package.
	for decoded, encoded := range map[string]string{
		"Images/photo_2026-10-17_19-10-08 (4).jpg": "Images/photo_2026-10-17_19-10-08%20(4).jpg",
		"Images/gernika-pikasso+.jpg":              "Images/gernika-pikasso+.jpg",
		"Images/609858@2x.webp":                    "Images/609858@2x.webp",
		"Images/Louis_(1822_-_1895),_chemist.jpg":  "Images/Louis_(1822_-_1895),_chemist.jpg",
		"Audio/Любовь.mp3":                         "Audio/%D0%9B%D1%8E%D0%B1%D0%BE%D0%B2%D1%8C.mp3",
		"Images/100%.png":                          "Images/100%25.png",
	} {
		if got := EncodeName(decoded); got != encoded {
			t.Errorf("EncodeName(%q) = %q, want %q", decoded, got, encoded)
		}
		if got := DecodeName(encoded); got != decoded {
			t.Errorf("DecodeName(%q) = %q, want %q", encoded, got, decoded)
		}
	}
	if got := DecodeName("Images/100%.png"); got != "Images/100%.png" {
		t.Errorf("invalid escape should be kept, got %q", got)
	}
}

func TestPackMinimal(t *testing.T) {
	b, err := ReadDir("../../testdata/minimal")
	if err != nil {
		t.Fatal(err)
	}
	warnings, err := Check(b)
	if err != nil || len(warnings) > 0 {
		t.Fatalf("check: %v %v", err, warnings)
	}
	out := filepath.Join(t.TempDir(), "min.siq")
	if err := WriteSIQ(out, b); err != nil {
		t.Fatal(err)
	}

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, fmt.Sprintf("%s:%d", f.Name, f.Method))
	}
	want := []string{"content.xml:8", "Images/cat.jpg:0", "Audio/meow.mp3:0"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("entries = %v, want %v", names, want)
	}

	xml, err := ReadContentXML(out)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte("fakejpg"))
	wantFiles := fmt.Sprintf(`<files><file name="Audio/meow.mp3" hash="%s" /><file name="Images/cat.jpg" hash="%X" /></files>`, Hash([]byte("fakemp3")), h)
	if !strings.Contains(string(xml), wantFiles) {
		t.Errorf("content.xml lacks %s\n%s", wantFiles, xml)
	}
	for _, want := range []string{`<right><answer>Cat</answer><answer>Kitty</answer></right><wrong><answer>Dog</answer></wrong>`, `<item type="audio" isRef="True" placement="background">meow.mp3</item>`} {
		if !strings.Contains(string(xml), want) {
			t.Errorf("content.xml lacks %s", want)
		}
	}

	back, err := ReadSIQ(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Package, b.Package) {
		t.Error("package changed after pack")
	}
}

func TestCheck(t *testing.T) {
	b, err := ReadDir("../../testdata/minimal")
	if err != nil {
		t.Fatal(err)
	}
	b.Files = append(b.Files, File{Path: "Images/unused.png"})
	b.Package.Logo = "@missing.png"
	if _, err := Check(b); err == nil || !strings.Contains(err.Error(), "Images/missing.png") {
		t.Errorf("expected missing logo error, got %v", err)
	}
	b.Package.Logo = ""
	warnings, err := Check(b)
	if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0], "unused.png") {
		t.Errorf("warnings = %v, err = %v", warnings, err)
	}
}

func TestFileHashesSkipNonMedia(t *testing.T) {
	got := FileHashes([]File{{Path: "quality.marker"}, {Path: "Video/b.mp4", Data: []byte("v")}, {Path: "Html/a.html", Data: []byte("h")}, {Path: "Images/sub/x.png"}})
	want := []siqxml.FileHash{{Name: "Html/a.html", Hash: Hash([]byte("h"))}, {Name: "Video/b.mp4", Hash: Hash([]byte("v"))}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestExamplePackIsComplete(t *testing.T) {
	b, err := ReadDir("../../example/example_pack")
	if err != nil {
		t.Fatal(err)
	}
	warnings, err := Check(b)
	if err != nil || len(warnings) > 0 {
		t.Fatalf("example package: %v %v", err, warnings)
	}
}
