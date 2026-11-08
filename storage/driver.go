// Package storage defines the StorageDriver and HashStore interfaces,
// and shared types used by all driver implementations.
package storage

import "io"

// PutResult is returned by StorageDriver.Put.
type PutResult struct {
	ID      string
	ETag    string
	Path    string // actual storage path (may differ from input path, e.g. external services)
	DupPath string // non-empty if content was already stored at this path (dedup)

	// URL and DeleteURL are optional external links set by backends whose
	// object lives on a third-party host with its own public URL (e.g. imgur).
	// They are empty for the azure/r2/local/p.sda1.dev/p.inari.site drivers,
	// which build their links from publicBaseURL. When URL is non-empty the
	// controller uses it directly instead of deriving a /<backend>/<path> link.
	URL       string
	DeleteURL string
}

// StorageDriver abstracts file storage operations.
type StorageDriver interface {
	Put(path, contentType string, body io.Reader) (*PutResult, error)
	Get(path string) (body io.ReadCloser, contentLength int64, contentType string, err error)
	Delete(path string) error
	Head(path string) (contentLength int64, contentType string, err error)
}

// HashStore maps content hashes to file paths for deduplication.
type HashStore interface {
	Lookup(md5hex string) (path string, err error)
	Store(md5hex, path string) error
}

// PathStore maps S3 paths to backend resource IDs.
type PathStore interface {
	Lookup(path string) (id string, err error)
	Store(path, id string) error
	Delete(path string) error
}
