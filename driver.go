package main

import (
	"crypto/md5"
	"encoding/hex"
	"io"

	"github.com/Hana-ame/azure-go/storage"
)

// AzureDriver adapts Agent to the storage.StorageDriver interface,
// with built-in MD5-based deduplication.
type AzureDriver struct {
	agent     *Agent
	hashStore storage.HashStore
	pathStore storage.PathStore
}

func NewAzureDriver(agent *Agent, hs storage.HashStore, ps storage.PathStore) *AzureDriver {
	return &AzureDriver{agent: agent, hashStore: hs, pathStore: ps}
}

func (d *AzureDriver) Put(path, contentType string, body io.Reader) (*storage.PutResult, error) {
	hash := md5.New()
	tee := io.TeeReader(body, hash)

	id, err := d.agent.Put(path, contentType, tee)
	if err != nil {
		return nil, err
	}

	md5hex := hex.EncodeToString(hash.Sum(nil))
	etag := `"` + md5hex + `"`

	if d.pathStore != nil {
		_ = d.pathStore.Store(path, id)
	}

	if d.hashStore != nil {
		if existingPath, err := d.hashStore.Lookup(md5hex); err == nil && existingPath != "" {
			d.agent.DeleteByPath(path)
			// Since we deduped, the S3 path should now point to the existing resource's ID.
			// But wait, we don't know the existing resource's ID easily without another lookup.
			// Let's assume we store the ID of the existing resource.
			// We can lookup the ID of existingPath.
			if d.pathStore != nil {
				if existingID, err := d.pathStore.Lookup(existingPath); err == nil && existingID != "" {
					_ = d.pathStore.Store(path, existingID)
				}
			}
			return &storage.PutResult{ID: id, ETag: etag, DupPath: existingPath}, nil
		}
		_ = d.hashStore.Store(md5hex, path)
	}

	return &storage.PutResult{ID: id, ETag: etag}, nil
}

func (d *AzureDriver) Get(path string) (io.ReadCloser, int64, string, error) {
	if d.pathStore != nil {
		if id, err := d.pathStore.Lookup(path); err == nil && id != "" {
			return d.agent.Get(id, "")
		}
	}
	return d.agent.GetByPath(path)
}
func (d *AzureDriver) Delete(path string) error {
	if d.pathStore != nil {
		if id, err := d.pathStore.Lookup(path); err == nil && id != "" {
			_ = d.pathStore.Delete(path)
			_, err := d.agent.Delete(id)
			return err
		}
	}
	return d.agent.DeleteByPath(path)
}
func (d *AzureDriver) Head(path string) (int64, string, error) {
	if d.pathStore != nil {
		if id, err := d.pathStore.Lookup(path); err == nil && id != "" {
			return d.agent.HeadByID(id)
		}
	}
	return d.agent.HeadByPath(path)
}
