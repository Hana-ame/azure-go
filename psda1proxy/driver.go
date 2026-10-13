package psda1proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/Hana-ame/azure-go/storage"
	psda1 "github.com/Hana-ame/azure-go/storage/p_sda1_dev"
)

// Driver implements storage.StorageDriver for p.sda1.dev with:
//   - Magic-byte format validation (webp/jpg/png/gif only)
//   - Upload via the underlying p_sda1_dev driver
type Driver struct {
	uploader *psda1.Driver
	client   *http.Client
}

// New creates a new psda1proxy Driver.
// The driver uses a no-proxy, IPv4-only HTTP client.
// Uploads are delegated to the standard p_sda1_dev driver.
func New() *Driver {
	return &Driver{
		uploader: psda1.New(),
		client:   NewClient(),
	}
}

// Put validates the image format (magic bytes), then uploads
// to p.sda1.dev.
//
// Returns the storage path assigned by p.sda1.dev (PutResult.Path).
func (d *Driver) Put(path, contentType string, body io.Reader) (*storage.PutResult, error) {
	// 1. Read body so we can inspect magic bytes AND upload the same data.
	buf, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("psda1proxy: read body: %w", err)
	}

	// 2. Validate format (reject avif, bmp, heic, non-images, etc.)
	format, ok := ValidateImage(bytes.NewReader(buf))
	if !ok {
		return nil, fmt.Errorf("%w – detected format: %q", ErrInvalidFormat, format)
	}

	// 3. Delegate to the underlying p.sda1.dev driver
	return d.uploader.Put(path, contentType, bytes.NewReader(buf))
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
