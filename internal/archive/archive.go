// Package archive reads and writes .siq archives and unpacked package folders.
package archive

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"siq_converter/internal/model"
	"siq_converter/internal/siqxml"
	"siq_converter/internal/siqyaml"
)

const (
	ContentXML  = "content.xml"
	ContentYAML = "content.yaml"
)

// File is a package file other than content.xml, e.g. "Images/photo 1.jpg".
// Its content is either held in Data or, for files of a package folder, read
// from Src on demand, so packing does not load all media into memory.
type File struct {
	Path     string // decoded, slash-separated, relative to the package root
	Data     []byte
	Src      string // file on disk; used when set
	Modified time.Time
}

// Open returns the file content.
func (f File) Open() (io.ReadCloser, error) {
	if f.Src != "" {
		return os.Open(f.Src)
	}
	return io.NopCloser(bytes.NewReader(f.Data)), nil
}

// Hash returns the uppercase hex SHA-256 of the file content.
func (f File) Hash() (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return "", fmt.Errorf("%s: %w", f.Path, err)
	}
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil))), nil
}

// Bundle is a package together with its files.
type Bundle struct {
	Package *model.Package
	Files   []File
}

// ReadSIQ loads a .siq archive.
func ReadSIQ(name string) (*Bundle, error) {
	zr, err := zip.OpenReader(name)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	b := &Bundle{}
	var content []byte
	seen := map[string]bool{}
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		data, err := readZipFile(f)
		if err != nil {
			return nil, err
		}
		if f.Name == ContentXML {
			content = data
			continue
		}
		p, err := cleanPath(DecodeName(f.Name))
		if err != nil {
			return nil, fmt.Errorf("archive entry %q: %w", f.Name, err)
		}
		// Folders on Windows and macOS ignore case, so names differing only in
		// case, or a stray content.yaml, would overwrite each other on unpack.
		key := strings.ToLower(p)
		if key == ContentYAML || key == ContentXML {
			return nil, fmt.Errorf("archive entry %q: a file named %s would clash with the package description", f.Name, p)
		}
		if seen[key] {
			return nil, fmt.Errorf("archive entry %q: duplicate file %q (names may differ only in case)", f.Name, p)
		}
		seen[key] = true
		b.Files = append(b.Files, File{Path: p, Data: data, Modified: f.Modified})
	}
	if content == nil {
		return nil, fmt.Errorf("%s: no %s in archive", name, ContentXML)
	}
	if b.Package, err = siqxml.Decode(content); err != nil {
		return nil, err
	}
	return b, nil
}

// ReadContentXML returns the raw content.xml of a .siq archive.
func ReadContentXML(name string) ([]byte, error) {
	zr, err := zip.OpenReader(name)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == ContentXML {
			return readZipFile(f)
		}
	}
	return nil, fmt.Errorf("%s: no %s in archive", name, ContentXML)
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// cleanPath rejects paths that would escape the package folder.
func cleanPath(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	c := path.Clean(p)
	if c == "." || path.IsAbs(c) || c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("unsafe path %q", p)
	}
	return c, nil
}

// WriteSIQ writes a .siq archive: content.xml first (deflated), then other
// root files, then the media folders; media is stored uncompressed.
// The archive is streamed into a temporary file next to name and moved into
// place only when complete, so a failure never leaves a broken archive behind.
func WriteSIQ(name string, b *Bundle) (err error) {
	hashes, err := FileHashes(b.Files)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(name), "."+filepath.Base(name)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	zw := zip.NewWriter(tmp)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: ContentXML, Method: zip.Deflate, Modified: time.Now()})
	if err != nil {
		return err
	}
	if _, err := w.Write(siqxml.Encode(b.Package, hashes)); err != nil {
		return err
	}
	for _, f := range sortedFiles(b.Files) {
		if err := writeEntry(zw, f); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), name)
}

func writeEntry(zw *zip.Writer, f File) error {
	method := zip.Store
	if !isMedia(f.Path) {
		method = zip.Deflate
	}
	mod := f.Modified
	if mod.IsZero() {
		mod = time.Now()
	}
	w, err := zw.CreateHeader(&zip.FileHeader{Name: EncodeName(f.Path), Method: method, Modified: mod})
	if err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	if _, err := io.Copy(w, rc); err != nil {
		return fmt.Errorf("%s: %w", f.Path, err)
	}
	return nil
}

// sortedFiles orders files as root files, Images, Audio, Video, Html, then
// anything else, each group sorted by path.
func sortedFiles(files []File) []File {
	rank := func(p string) int {
		dir, _, found := strings.Cut(p, "/")
		if !found {
			return 0
		}
		for i, f := range model.MediaFolders {
			if dir == f {
				return i + 1
			}
		}
		return len(model.MediaFolders) + 1
	}
	out := append([]File(nil), files...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i].Path), rank(out[j].Path)
		if ri != rj {
			return ri < rj
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// isMedia reports whether p is a file directly inside a media folder.
func isMedia(p string) bool {
	dir, rest, found := strings.Cut(p, "/")
	if !found || strings.Contains(rest, "/") {
		return false
	}
	for _, f := range model.MediaFolders {
		if dir == f {
			return true
		}
	}
	return false
}

// FileHashes computes the <files> list the way SIGame does: uppercase hex
// SHA-256 of every media file, keyed by "Folder/decoded name", sorted ordinally.
func FileHashes(files []File) ([]siqxml.FileHash, error) {
	var out []siqxml.FileHash
	for _, f := range files {
		if !isMedia(f.Path) {
			continue
		}
		h, err := f.Hash()
		if err != nil {
			return nil, err
		}
		out = append(out, siqxml.FileHash{Name: f.Path, Hash: h})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Hash returns the uppercase hex SHA-256 of data.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// EncodeName percent-encodes an archive entry name the way SIGame packages
// do: every byte except unreserved and a few safe characters is escaped.
func EncodeName(p string) string {
	const safe = "-._~()!*',+@/"
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(safe, c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// DecodeName reverses EncodeName; names that are not valid escapes are kept as is.
func DecodeName(name string) string {
	if d, err := url.PathUnescape(name); err == nil {
		return d
	}
	return name
}

// ReadDir loads an unpacked package folder: content.yaml plus every other
// non-hidden file.
func ReadDir(dir string) (*Bundle, error) {
	data, err := os.ReadFile(filepath.Join(dir, ContentYAML))
	if err != nil {
		return nil, err
	}
	b := &Bundle{}
	if b.Package, err = siqyaml.Decode(data); err != nil {
		return nil, fmt.Errorf("%s: %w", ContentYAML, err)
	}
	// WalkDir does not follow a symlinked root, so resolve it first.
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != dir && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		// Case-insensitive filesystems open "Content.yaml" as content.yaml.
		if strings.EqualFold(rel, ContentYAML) || strings.EqualFold(rel, ContentXML) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b.Files = append(b.Files, File{Path: rel, Src: p, Modified: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return b, nil
}

// WriteDir writes content.yaml and all files into dir, which must not exist or be empty.
func WriteDir(dir string, b *Bundle) error {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s: directory is not empty", dir)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	y, err := siqyaml.Encode(b.Package)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, ContentYAML), y, 0o644); err != nil {
		return err
	}
	for _, f := range b.Files {
		if strings.EqualFold(f.Path, ContentYAML) {
			return fmt.Errorf("%s: a package file would overwrite %s", f.Path, ContentYAML)
		}
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := copyTo(p, f); err != nil {
			return err
		}
		if !f.Modified.IsZero() {
			_ = os.Chtimes(p, f.Modified, f.Modified)
		}
	}
	return nil
}

func copyTo(dst string, f File) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return fmt.Errorf("%s: %w", f.Path, err)
	}
	return out.Close()
}

// Check verifies that every file the package references exists. It returns
// an error listing missing files, and warnings for media files nothing references.
func Check(b *Bundle) (warnings []string, err error) {
	have := map[string]bool{}
	for _, f := range b.Files {
		have[f.Path] = true
	}
	used := map[string]bool{}
	var missing []string
	need := func(p, what string) {
		if used[p] {
			return
		}
		used[p] = true
		if !have[p] {
			missing = append(missing, fmt.Sprintf("%s (%s)", p, what))
		}
	}
	if logo, ok := strings.CutPrefix(b.Package.Logo, "@"); ok {
		need(model.FolderImages+"/"+logo, "logo")
	}
	b.Package.WalkItems(func(it model.Item) {
		if !it.IsRef {
			return
		}
		if folder, ok := model.MediaFolder(it.Type); ok {
			need(folder+"/"+it.Value, it.Type)
		}
	})
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing referenced files:\n  %s", strings.Join(missing, "\n  "))
	}
	for _, f := range b.Files {
		if isMedia(f.Path) && !used[f.Path] {
			warnings = append(warnings, "unreferenced file: "+f.Path)
		}
	}
	return warnings, nil
}
