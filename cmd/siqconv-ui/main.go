// Command siqconv-ui is a small desktop window for siqconv: unpack a .siq into
// an editable folder, pack a folder back into a .siq, or create a new package.
package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"siq_converter/internal/archive"
	"siq_converter/internal/ops"
)

// version is set by release builds: -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	a := app.NewWithID("io.github.siq_converter")
	w := a.NewWindow("SIQ Converter " + version)
	u := &ui{app: a, win: w}
	w.SetContent(u.build())
	w.SetOnDropped(u.dropped)
	w.Resize(fyne.NewSize(760, 540))
	w.ShowAndRun()
}

type ui struct {
	app  fyne.App
	win  fyne.Window
	tabs *container.AppTabs

	unpackIn, unpackOut *widget.Entry
	packIn, packOut     *widget.Entry
	newParent, newName  *widget.Entry

	actions []*widget.Button
	busy    *widget.ProgressBarInfinite
	log     *widget.Label
	logBox  *container.Scroll
	openBtn *widget.Button
	lastOut string
}

func (u *ui) build() fyne.CanvasObject {
	u.busy = widget.NewProgressBarInfinite()
	u.busy.Stop()
	u.busy.Hide()
	u.log = widget.NewLabel("Drop a .siq file or a package folder here, or pick one above.")
	u.log.Wrapping = fyne.TextWrapWord
	u.logBox = container.NewVScroll(u.log)
	u.logBox.SetMinSize(fyne.NewSize(0, 140))
	u.openBtn = widget.NewButtonWithIcon("Show result", theme.FolderOpenIcon(), u.showResult)
	u.openBtn.Disable()

	u.tabs = container.NewAppTabs(
		container.NewTabItemWithIcon("SIQ to folder", theme.DownloadIcon(), u.unpackTab()),
		container.NewTabItemWithIcon("Folder to SIQ", theme.UploadIcon(), u.packTab()),
		container.NewTabItemWithIcon("New package", theme.ContentAddIcon(), u.newTab()),
	)
	bottom := container.NewBorder(nil, container.NewBorder(nil, nil, nil, u.openBtn, u.busy), nil, nil, u.logBox)
	return container.NewBorder(nil, nil, nil, nil, container.NewVSplit(u.tabs, bottom))
}

// --- tabs -------------------------------------------------------------------

func (u *ui) unpackTab() fyne.CanvasObject {
	u.unpackIn = widget.NewEntry()
	u.unpackIn.SetPlaceHolder("package.siq")
	u.unpackOut = widget.NewEntry()
	u.unpackOut.SetPlaceHolder("folder to create (default: next to the .siq)")
	autoFill(u.unpackIn, u.unpackOut, func(s string) string {
		if strings.EqualFold(filepath.Ext(s), ".siq") {
			return ops.UnpackTarget(s)
		}
		return ""
	})
	browseIn := widget.NewButtonWithIcon("Browse…", theme.FileIcon(), func() {
		d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if r != nil {
				r.Close()
				u.unpackIn.SetText(r.URI().Path())
			}
		}, u.win)
		d.SetFilter(storage.NewExtensionFileFilter([]string{".siq"}))
		u.showFileDialog(d, u.unpackIn.Text)
	})
	browseOut := widget.NewButtonWithIcon("Browse…", theme.FolderIcon(), func() {
		u.pickFolder(u.unpackOut.Text, func(dir string) {
			name := filepath.Base(ops.UnpackTarget(u.unpackIn.Text))
			if name == "." || name == "" {
				name = "package"
			}
			u.unpackOut.SetText(filepath.Join(dir, name))
		})
	})
	run := u.action("Unpack", theme.DownloadIcon(), func() { u.runUnpack(false) })
	verify := u.action("Verify .siq", theme.ConfirmIcon(), func() {
		in := u.unpackIn.Text
		if in == "" {
			u.fail(errors.New("choose a .siq file first"))
			return
		}
		u.do("Verifying "+in+"…", func() (string, string, error) {
			s, err := ops.Verify(in)
			return s, "", err
		})
	})
	return container.NewVBox(
		hint("Turn a .siq into a folder with content.yaml (edit it in any text editor) and media files."),
		widget.NewForm(
			widget.NewFormItem(".siq file", container.NewBorder(nil, nil, nil, browseIn, u.unpackIn)),
			widget.NewFormItem("Output folder", container.NewBorder(nil, nil, nil, browseOut, u.unpackOut)),
		),
		container.NewHBox(run, verify),
	)
}

func (u *ui) packTab() fyne.CanvasObject {
	u.packIn = widget.NewEntry()
	u.packIn.SetPlaceHolder("folder with content.yaml")
	u.packOut = widget.NewEntry()
	u.packOut.SetPlaceHolder("package.siq (default: next to the folder)")
	autoFill(u.packIn, u.packOut, func(s string) string {
		if s == "" {
			return ""
		}
		return ops.PackTarget(s)
	})
	browseIn := widget.NewButtonWithIcon("Browse…", theme.FolderIcon(), func() {
		u.pickFolder(u.packIn.Text, func(dir string) { u.packIn.SetText(dir) })
	})
	// A folder picker rather than a save dialog: Fyne's save dialog creates (and
	// so truncates) the chosen file immediately. The name stays editable, and
	// Pack asks before replacing an existing archive.
	browseOut := widget.NewButtonWithIcon("Browse…", theme.FolderIcon(), func() {
		u.pickFolder(u.packOut.Text, func(dir string) {
			name := filepath.Base(u.packOut.Text)
			if !strings.EqualFold(filepath.Ext(name), ".siq") {
				name = filepath.Base(ops.PackTarget(u.packIn.Text))
			}
			u.packOut.SetText(filepath.Join(dir, name))
		})
	})
	run := u.action("Pack", theme.UploadIcon(), func() { u.runPack(false) })
	return container.NewVBox(
		hint("Turn a package folder (content.yaml + Images/Audio/Video/Html) into a .siq for SIGame."),
		widget.NewForm(
			widget.NewFormItem("Package folder", container.NewBorder(nil, nil, nil, browseIn, u.packIn)),
			widget.NewFormItem(".siq file", container.NewBorder(nil, nil, nil, browseOut, u.packOut)),
		),
		container.NewHBox(run),
	)
}

func (u *ui) newTab() fyne.CanvasObject {
	u.newParent = widget.NewEntry()
	if home, err := os.UserHomeDir(); err == nil {
		u.newParent.SetText(filepath.Join(home, "Documents"))
	}
	u.newName = widget.NewEntry()
	u.newName.SetText("my_package")
	browse := widget.NewButtonWithIcon("Browse…", theme.FolderIcon(), func() {
		u.pickFolder(u.newParent.Text, func(dir string) { u.newParent.SetText(dir) })
	})
	create := u.action("Create", theme.ContentAddIcon(), func() {
		name := strings.TrimSpace(u.newName.Text)
		if name == "" || strings.ContainsAny(name, `/\:*?"<>|`) {
			u.fail(errors.New("enter a folder name without / \\ : * ? \" < > |"))
			return
		}
		dir := filepath.Join(u.newParent.Text, name)
		u.do("Creating "+dir+"…", func() (string, string, error) {
			if err := ops.Init(dir); err != nil {
				return "", "", err
			}
			msg := fmt.Sprintf("Created %s.\n\nNext:\n1. Open %s in a text editor and write your questions.\n"+
				"2. Put media files into Images, Audio, Video and Html (the comment at the top of content.yaml lists the example ones).\n"+
				"3. Use the \"Folder to SIQ\" tab to build the .siq.", dir, archive.ContentYAML)
			fyne.Do(func() { u.packIn.SetText(dir) })
			return msg, dir, nil
		})
	})
	return container.NewVBox(
		hint("Create a new package folder with a commented example question of every type."),
		widget.NewForm(
			widget.NewFormItem("Create in", container.NewBorder(nil, nil, nil, browse, u.newParent)),
			widget.NewFormItem("Folder name", u.newName),
		),
		container.NewHBox(create),
	)
}

// --- operations -------------------------------------------------------------

func (u *ui) runUnpack(force bool) {
	in, out := u.unpackIn.Text, u.unpackOut.Text
	if in == "" {
		u.fail(errors.New("choose a .siq file first"))
		return
	}
	u.do("Unpacking "+in+"…", func() (string, string, error) {
		res, err := ops.Unpack(in, out, force)
		if errors.Is(err, ops.ErrExists) {
			fyne.Do(func() {
				u.confirmOverwrite(fmt.Sprintf("The folder\n%s\nis not empty. Delete it and unpack again?", outOrDefault(out, ops.UnpackTarget(in))),
					func() { u.runUnpack(true) })
			})
			return "Not unpacked: the output folder is not empty.", "", nil
		}
		if err != nil {
			return "", "", err
		}
		return fmt.Sprintf("Unpacked %s\ninto %s (%d files).\nEdit content.yaml there, then pack it with the \"Folder to SIQ\" tab.", in, res.Out, res.Files), res.Out, nil
	})
}

func (u *ui) runPack(force bool) {
	in, out := u.packIn.Text, u.packOut.Text
	if in == "" {
		u.fail(errors.New("choose a package folder first"))
		return
	}
	u.do("Packing "+in+"…", func() (string, string, error) {
		res, err := ops.Pack(in, out, force)
		if errors.Is(err, ops.ErrExists) {
			fyne.Do(func() {
				u.confirmOverwrite(fmt.Sprintf("The file\n%s\nalready exists. Replace it?", outOrDefault(out, ops.PackTarget(in))),
					func() { u.runPack(true) })
			})
			return "Not packed: the .siq file already exists.", "", nil
		}
		if err != nil {
			return "", "", err
		}
		msg := fmt.Sprintf("Packed %s\ninto %s (%d files). Open it in SIGame.", in, res.Out, res.Files)
		for _, w := range res.Warnings {
			msg += "\nWarning: " + w
		}
		return msg, res.Out, nil
	})
}

func outOrDefault(out, def string) string {
	if out == "" {
		return def
	}
	return out
}

// do runs fn in the background with the action buttons disabled. fn returns
// the message to log and, optionally, a path for the "Show result" button.
func (u *ui) do(start string, fn func() (msg, result string, err error)) {
	u.setBusy(true)
	u.setLog(start)
	go func() {
		msg, result, err := fn()
		fyne.Do(func() {
			u.setBusy(false)
			if err != nil {
				u.fail(err)
				return
			}
			u.setLog(msg)
			if result != "" {
				u.lastOut = result
				u.openBtn.Enable()
			}
		})
	}()
}

func (u *ui) action(label string, icon fyne.Resource, fn func()) *widget.Button {
	b := widget.NewButtonWithIcon(label, icon, fn)
	b.Importance = widget.HighImportance
	u.actions = append(u.actions, b)
	return b
}

func (u *ui) setBusy(busy bool) {
	for _, b := range u.actions {
		if busy {
			b.Disable()
		} else {
			b.Enable()
		}
	}
	if busy {
		u.openBtn.Disable()
		u.busy.Show()
		u.busy.Start()
	} else {
		u.busy.Stop()
		u.busy.Hide()
	}
}

func (u *ui) setLog(s string) {
	u.log.SetText(s)
	u.logBox.ScrollToTop()
}

func (u *ui) fail(err error) {
	u.setLog("Error: " + err.Error())
	dialog.ShowError(err, u.win)
}

func (u *ui) confirmOverwrite(msg string, yes func()) {
	dialog.ShowConfirm("Overwrite?", msg, func(ok bool) {
		if ok {
			yes()
		}
	}, u.win)
}

// showResult opens the produced folder, or the folder containing the produced file.
func (u *ui) showResult() {
	target := u.lastOut
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		target = filepath.Dir(target)
	}
	link, err := url.Parse(storage.NewFileURI(target).String())
	if err == nil {
		err = u.app.OpenURL(link)
	}
	if err != nil {
		u.fail(err)
	}
}

// --- dialogs and drag & drop ------------------------------------------------

func (u *ui) pickFolder(start string, picked func(dir string)) {
	d := dialog.NewFolderOpen(func(l fyne.ListableURI, err error) {
		if l != nil {
			picked(l.Path())
		}
	}, u.win)
	u.showFileDialog(d, start)
}

// showFileDialog opens d at the folder of start (or its nearest existing parent), sized to the window.
func (u *ui) showFileDialog(d *dialog.FileDialog, start string) {
	for dir := start; dir != "" && dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			if l, err := storage.ListerForURI(storage.NewFileURI(dir)); err == nil {
				d.SetLocation(l)
			}
			break
		}
	}
	d.Show() // before Resize: Fyne's FileDialog.Resize panics on a dialog not yet shown
	d.Resize(u.win.Canvas().Size())
}

// dropped fills the matching tab: a .siq goes to "SIQ to folder", a folder (or its content.yaml) to "Folder to SIQ".
func (u *ui) dropped(_ fyne.Position, uris []fyne.URI) {
	if len(uris) == 0 {
		return
	}
	path := uris[0].Path()
	switch {
	case strings.EqualFold(filepath.Ext(path), ".siq"):
		u.unpackIn.SetText(path)
		u.tabs.SelectIndex(0)
	case filepath.Base(path) == archive.ContentYAML:
		u.packIn.SetText(filepath.Dir(path))
		u.tabs.SelectIndex(1)
	default:
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			u.packIn.SetText(path)
			u.tabs.SelectIndex(1)
			return
		}
		u.setLog("Drop a .siq file, a package folder or its content.yaml.")
	}
}

// autoFill keeps out derived from in until the user picks or types another
// value for out: only an empty field or the previous suggestion is replaced.
func autoFill(in, out *widget.Entry, suggest func(string) string) {
	last := ""
	in.OnChanged = func(s string) {
		next := suggest(s)
		if next == "" || (out.Text != "" && out.Text != last) {
			return
		}
		last = next
		out.SetText(next)
	}
}

func hint(s string) fyne.CanvasObject {
	l := widget.NewLabel(s)
	l.Wrapping = fyne.TextWrapWord
	l.Importance = widget.LowImportance
	return l
}
