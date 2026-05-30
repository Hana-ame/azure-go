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
}

func NewAzureDriver(agent *Agent, hs storage.HashStore) *AzureDriver {
	return &AzureDriver{agent: agent, hashStore: hs}
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

	if d.hashStore != nil {
		if existingPath, err := d.hashStore.Lookup(md5hex); err == nil && existingPath != "" {
			d.agent.DeleteByPath(path)
			return &storage.PutResult{ID: id, ETag: etag, DupPath: existingPath}, nil
		}
		_ = d.hashStore.Store(md5hex, path)
	}

	return &storage.PutResult{ID: id, ETag: etag}, nil
}

func (d *AzureDriver) Get(path string) (io.ReadCloser, int64, string, error) {
	return d.agent.GetByPath(path)
}
func (d *AzureDriver) Delete(path string) error {
	return d.agent.DeleteByPath(path)
}
func (d *AzureDriver) Head(path string) (int64, string, error) {
	return d.agent.HeadByPath(path)
}
