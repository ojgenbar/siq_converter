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
type File struct {
	Path     string // decoded, slash-separated, relative to the package root
	Data     []byte
	Modified time.Time
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
		if seen[p] {
			return nil, fmt.Errorf("archive entry %q: duplicate file %q", f.Name, p)
		}
		seen[p] = true
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
func WriteSIQ(name string, b *Bundle) error {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	content := siqxml.Encode(b.Package, FileHashes(b.Files))
	w, err := zw.CreateHeader(&zip.FileHeader{Name: ContentXML, Method: zip.Deflate, Modified: time.Now()})
	if err != nil {
		return err
	}
	if _, err := w.Write(content); err != nil {
		return err
	}
	for _, f := range sortedFiles(b.Files) {
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
		if _, err := w.Write(f.Data); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(name, buf.Bytes(), 0o644)
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
func FileHashes(files []File) []siqxml.FileHash {
	var out []siqxml.FileHash
	for _, f := range files {
		if !isMedia(f.Path) {
			continue
		}
		out = append(out, siqxml.FileHash{Name: f.Path, Hash: Hash(f.Data)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
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
		if rel == ContentYAML || rel == ContentXML {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b.Files = append(b.Files, File{Path: rel, Data: data, Modified: info.ModTime()})
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
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, f.Data, 0o644); err != nil {
			return err
		}
		if !f.Modified.IsZero() {
			_ = os.Chtimes(p, f.Modified, f.Modified)
		}
	}
	return nil
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
