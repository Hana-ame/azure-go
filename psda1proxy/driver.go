package psda1proxy

import (
	"io"
	"net/http"

	"github.com/Hana-ame/azure-go/storage"
	psda1 "github.com/Hana-ame/azure-go/storage/p_sda1_dev"
)

// Driver implements storage.StorageDriver for p.sda1.dev by streaming directly
// to the underlying p_sda1_dev driver.
type Driver struct {
	uploader *psda1.Driver
	client   *http.Client
}

// New creates a new psda1proxy Driver.
// The driver uses a no-proxy, IPv4-only HTTP client.
// Uploads are delegated directly to the standard p_sda1_dev driver.
func New() *Driver {
	return &Driver{
		uploader: psda1.New(),
		client:   NewClient(),
	}
}

// Put streams the raw body directly to p.sda1.dev without buffering in memory.
//
// Returns the storage path assigned by p.sda1.dev (PutResult.Path).
func (d *Driver) Put(path, contentType string, body io.Reader) (*storage.PutResult, error) {
	return d.uploader.Put(path, contentType, body)
}

// Get delegates to the underlying driver (used by legacy /p.sda1.dev/*path
// route). The new proxy route (/api/p.sda1.dev/*path) uses ProxyHandler
// instead and does NOT go through this method.
func (d *Driver) Get(path string) (io.ReadCloser, int64, string, error) {
	return d.uploader.Get(path)
}

// Delete delegates to the underlying driver. p.sda1.dev does not support
// delete via its upload API, so this returns an error. Delete links are
// generated as direct URLs (https://p.sda1.dev/…) that bypass the proxy.
func (d *Driver) Delete(path string) error {
	return d.uploader.Delete(path)
}

// Head delegates to the underlying driver.
func (d *Driver) Head(path string) (int64, string, error) {
	return d.uploader.Head(path)
}
