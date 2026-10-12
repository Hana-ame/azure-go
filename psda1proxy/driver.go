package psda1proxy

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/Hana-ame/azure-go/storage"
	psda1 "github.com/Hana-ame/azure-go/storage/p_sda1_dev"
)

// Driver implements storage.StorageDriver for p.sda1.dev with:
//   - Magic-byte format validation (webp/jpg/png only)
//   - Optional China IP enforcement (env PSA1_ENFORCE_CHINA_IP=true)
//   - Upload via the underlying p_sda1_dev driver
type Driver struct {
	uploader *psda1.Driver
	client   *http.Client
	enforceIP bool

	// --- IP check state (checked once) ---
	mu        sync.Mutex
	ipChecked bool
	ipChina   bool
	ip        string
	ipErr     error
}

// New creates a new psda1proxy Driver.
// The driver uses a no-proxy, IPv4-only HTTP client for the IP check.
// Uploads are delegated to the standard p_sda1_dev driver.
//
// Set PSA1_ENFORCE_CHINA_IP=true to reject uploads when the outbound IP
// is not in China. Default: warn only (IP is logged but uploads proceed).
func New() *Driver {
	enforce := os.Getenv("PSA1_ENFORCE_CHINA_IP") == "true"
	return &Driver{
		uploader:  psda1.New(),
		client:    NewClient(),
		enforceIP: enforce,
	}
}

// Put validates the image format (magic bytes), optionally checks that
// the outbound IP is in China (if enforceIP is enabled), then uploads
// to p.sda1.dev.
//
// Returns the storage path assigned by p.sda1.dev (PutResult.Path).
func (d *Driver) Put(path, contentType string, body io.Reader) (*storage.PutResult, error) {
	// 1. Optional: enforce China-only IP
	if d.enforceIP {
		if err := d.ensureChinaIP(); err != nil {
			return nil, err
		}
	} else {
		// Non-blocking: check IP for logging only
		go d.checkIPLazy()
	}

	// 2. Read body so we can inspect magic bytes AND upload the same data.
	buf, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("psda1proxy: read body: %w", err)
	}

	// 3. Validate format (reject avif, gif, bmp, heic, etc.)
	format, ok := ValidateImage(bytes.NewReader(buf))
	if !ok {
		return nil, fmt.Errorf("%w – detected format: %q", ErrInvalidFormat, format)
	}

	// 4. Delegate to the underlying p.sda1.dev driver
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

// ensureChinaIP checks the outbound IP exactly once (first call) and
// returns an error if the IP is not in China. The result is cached
// for the lifetime of the Driver instance.
func (d *Driver) ensureChinaIP() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.ipChecked {
		if d.ipErr != nil {
			return d.ipErr
		}
		if !d.ipChina {
			return fmt.Errorf("psda1proxy: outbound IP is not in China (IP: %s) – PSA1_ENFORCE_CHINA_IP=true, uploads require China network", d.ip)
		}
		return nil
	}

	ip, isChina, err := CheckChinaIP(d.client)
	d.ip = ip
	d.ipChina = isChina
	d.ipErr = err
	d.ipChecked = true

	if err != nil {
		return fmt.Errorf("psda1proxy: IP verification failed: %w", err)
	}
	if !isChina {
		return fmt.Errorf("psda1proxy: outbound IP is not in China (IP: %s) – PSA1_ENFORCE_CHINA_IP=true, uploads require China network", ip)
	}
	return nil
}

// checkIPLazy performs a non-blocking IP check for logging purposes.
// Used when enforceIP is disabled.
func (d *Driver) checkIPLazy() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.ipChecked {
		return
	}

	ip, isChina, err := CheckChinaIP(d.client)
	d.ip = ip
	d.ipChina = isChina
	d.ipErr = err
	d.ipChecked = true

	if err != nil {
		log.Printf("[psda1proxy] IP check failed (non-blocking): %v", err)
	} else if !isChina {
		log.Printf("[psda1proxy] WARNING: outbound IP %s is not in China – uploads will proceed but may fail if upstream requires China IP", ip)
	} else {
		log.Printf("[psda1proxy] IP check passed: %s (China)", ip)
	}
}
