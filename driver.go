package main

import (
	"crypto/md5"
	"encoding/hex"
	"io"
)

// PutResult is returned by StorageDriver.Put.
type PutResult struct {
	ID      string // file ID from storage
	ETag    string // MD5 hex, with quotes
	DupPath string // non-empty if content was already stored at this path
}

// StorageDriver abstracts file storage operations.
// Each driver handles its own dedup internally.
type StorageDriver interface {
	Put(path, contentType string, body io.Reader) (*PutResult, error)
	Get(path string) (body io.ReadCloser, contentLength int64, contentType string, err error)
	Delete(path string) error
	Head(path string) (contentLength int64, contentType string, err error)
}

// --- AzureDriver ---

type AzureDriver struct {
	agent     *Agent
	hashStore HashStore
}

func NewAzureDriver(agent *Agent, hs HashStore) *AzureDriver {
	return &AzureDriver{agent: agent, hashStore: hs}
}

func (d *AzureDriver) Put(path, contentType string, body io.Reader) (*PutResult, error) {
	// 流式计算 MD5 同时上传
	hash := md5.New()
	tee := io.TeeReader(body, hash)

	id, err := d.agent.Put(path, contentType, tee)
	if err != nil {
		return nil, err
	}

	md5hex := hex.EncodeToString(hash.Sum(nil))
	etag := `"` + md5hex + `"`

	// 去重：查 hash → 命中则删新文件，返回旧路径
	if d.hashStore != nil {
		if existingPath, lookupErr := d.hashStore.Lookup(md5hex); lookupErr == nil && existingPath != "" {
			d.agent.DeleteByPath(path) // 删除刚上传的重复文件
			return &PutResult{ID: id, ETag: etag, DupPath: existingPath}, nil
		}
		_ = d.hashStore.Store(md5hex, path)
	}

	return &PutResult{ID: id, ETag: etag}, nil
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
