// Package scaffold creates a new package folder with a commented example
// content.yaml covering every question type.
package scaffold

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"siq_converter/internal/archive"
	"siq_converter/internal/model"
)

//go:embed template.yaml
var template string

// Content returns the example content.yaml for a package with the given name.
func Content(name string, now time.Time) (string, error) {
	quoted, err := yaml.Marshal(name)
	if err != nil {
		return "", err
	}
	return strings.NewReplacer(
		"{{NAME}}", strings.TrimSuffix(string(quoted), "\n"),
		"{{DATE}}", now.Format("02.01.2006"),
	).Replace(template), nil
}

// Init creates dir (which must not exist or be empty) with content.yaml and
// empty media folders. The package is named after the folder.
func Init(dir string) error {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s: directory is not empty", dir)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	content, err := Content(filepath.Base(filepath.Clean(dir)), time.Now())
	if err != nil {
		return err
	}
	for _, folder := range model.MediaFolders {
		if err := os.MkdirAll(filepath.Join(dir, folder), 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, archive.ContentYAML), []byte(content), 0o644)
}
