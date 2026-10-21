package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/test"
)

// TestUI builds the window with Fyne's headless driver, checks drag & drop
// routing, and renders every tab. Set SIQCONV_UI_SHOTS=<dir> to keep the PNGs.
func TestUI(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := test.NewWindow(nil)
	u := &ui{app: a, win: w}
	w.SetContent(u.build())
	w.Resize(fyne.NewSize(760, 540))

	example, err := filepath.Abs("../../example/example_pack")
	if err != nil {
		t.Fatal(err)
	}
	u.dropped(fyne.Position{}, []fyne.URI{storage.NewFileURI(filepath.Join(example, "content.yaml"))})
	if u.packIn.Text != example || u.packOut.Text != example+".siq" || u.tabs.SelectedIndex() != 1 {
		t.Errorf("dropping content.yaml: packIn=%q packOut=%q tab=%d", u.packIn.Text, u.packOut.Text, u.tabs.SelectedIndex())
	}
	u.dropped(fyne.Position{}, []fyne.URI{storage.NewFileURI("/tmp/some pack.siq")})
	if u.unpackIn.Text != "/tmp/some pack.siq" || u.unpackOut.Text != "/tmp/some pack" || u.tabs.SelectedIndex() != 0 {
		t.Errorf("dropping .siq: unpackIn=%q unpackOut=%q tab=%d", u.unpackIn.Text, u.unpackOut.Text, u.tabs.SelectedIndex())
	}

	// Opening the Browse dialogs must not panic (Resize before Show did).
	u.pickFolder(t.TempDir(), func(string) {})
	u.showFileDialog(dialog.NewFileOpen(func(fyne.URIReadCloser, error) {}, w), t.TempDir())

	// A chosen output folder survives picking another input.
	u.unpackOut.SetText("/chosen/folder")
	u.dropped(fyne.Position{}, []fyne.URI{storage.NewFileURI("/tmp/other.siq")})
	if u.unpackOut.Text != "/chosen/folder" {
		t.Errorf("chosen output was replaced by %q", u.unpackOut.Text)
	}

	u.newName.SetText("Банановая солянка")
	dir := os.Getenv("SIQCONV_UI_SHOTS")
	if dir == "" {
		dir = t.TempDir()
	}
	for i, name := range []string{"unpack", "pack", "new"} {
		u.tabs.SelectIndex(i)
		f, err := os.Create(filepath.Join(dir, "ui_"+name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, w.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}

// TestUIFlows runs unpack and pack through the UI handlers, including the
// "already exists" path that asks before overwriting.
func TestUIFlows(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := test.NewWindow(nil)
	u := &ui{app: a, win: w}
	w.SetContent(u.build())

	tmp := t.TempDir()
	siq, err := filepath.Abs("../../example/example_pack.siq")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(siq); err != nil {
		t.Skip("example_pack.siq not built; run: siqconv pack example/example_pack")
	}

	u.unpackIn.SetText(siq)
	u.unpackOut.SetText(filepath.Join(tmp, "unpacked"))
	u.runUnpack(false)
	waitLog(t, u, "Unpacked")

	u.packIn.SetText(filepath.Join(tmp, "unpacked"))
	u.packOut.SetText(filepath.Join(tmp, "again.siq"))
	u.runPack(false)
	waitLog(t, u, "Packed")
	if u.lastOut != filepath.Join(tmp, "again.siq") {
		t.Errorf("lastOut = %q", u.lastOut)
	}

	u.runPack(false)
	waitLog(t, u, "Not packed")
}

func waitLog(t *testing.T, u *ui, prefix string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var text string
		fyne.DoAndWait(func() { text = u.log.Text })
		if strings.HasPrefix(text, prefix) {
			return
		}
		if strings.HasPrefix(text, "Error") {
			t.Fatalf("waiting for %q: %s", prefix, text)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q, log: %s", prefix, u.log.Text)
}
