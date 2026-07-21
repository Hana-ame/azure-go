// Package local implements a local-filesystem StorageDriver.
package local

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"

	"github.com/Hana-ame/azure-go/storage"
)

// Driver stores files under a root directory on the local filesystem.
type Driver struct {
	root string
}

// New creates a new local Driver rooted at dir.
// The directory is created if it does not exist.
func New(root string) (*Driver, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("local driver: %w", err)
	}
	return &Driver{root: root}, nil
}

func (d *Driver) Put(path, contentType string, body io.Reader) (*storage.PutResult, error) {
	fullPath := filepath.Join(d.root, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return nil, err
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	hash := md5.New()
	tee := io.TeeReader(body, hash)

	if _, err := io.Copy(f, tee); err != nil {
		return nil, err
	}

	etag := `"` + hex.EncodeToString(hash.Sum(nil)) + `"`
	return &storage.PutResult{ID: path, ETag: etag}, nil
}

func (d *Driver) Get(path string) (io.ReadCloser, int64, string, error) {
	fullPath := filepath.Join(d.root, path)
	f, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, "", fmt.Errorf("not found: %s", path)
		}
		return nil, 0, "", err
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, "", err
	}

	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return f, info.Size(), contentType, nil
}

func (d *Driver) Delete(path string) error {
	fullPath := filepath.Join(d.root, path)
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (d *Driver) Head(path string) (int64, string, error) {
	fullPath := filepath.Join(d.root, path)
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, "", fmt.Errorf("not found: %s", path)
		}
		return 0, "", err
	}
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return info.Size(), contentType, nil
}
