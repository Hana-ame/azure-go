package r2

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/Hana-ame/azure-go/storage"
	"github.com/minio/minio-go/v7"
)

type Driver struct {
	client *minio.Client
	bucket string
	prefix string
}

func New(client *minio.Client, bucket, prefix string) *Driver {
	p := strings.Trim(prefix, "/")
	if p != "" {
		p += "/"
	}
	return &Driver{client: client, bucket: bucket, prefix: p}
}

func (d *Driver) Put(path, contentType string, body io.Reader) (*storage.PutResult, error) {
	fullPath := d.prefix + path

	hash := md5.New()
	tee := io.TeeReader(body, hash)

	_, err := d.client.PutObject(context.Background(), d.bucket, fullPath, tee, -1, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return nil, fmt.Errorf("r2 put: %w", err)
	}

	md5hex := hex.EncodeToString(hash.Sum(nil))
	etag := `"` + md5hex + `"`
	return &storage.PutResult{ID: fullPath, ETag: etag}, nil
}

func (d *Driver) Get(path string) (io.ReadCloser, int64, string, error) {
	fullPath := d.prefix + path

	obj, err := d.client.GetObject(context.Background(), d.bucket, fullPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, "", fmt.Errorf("r2 get: %w", err)
	}

	stat, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, 0, "", fmt.Errorf("r2 get stat: %w", err)
	}

	return obj, stat.Size, stat.ContentType, nil
}

func (d *Driver) Delete(path string) error {
	fullPath := d.prefix + path

	err := d.client.RemoveObject(context.Background(), d.bucket, fullPath, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("r2 delete: %w", err)
	}
	return nil
}

func (d *Driver) Head(path string) (int64, string, error) {
	fullPath := d.prefix + path

	stat, err := d.client.StatObject(context.Background(), d.bucket, fullPath, minio.StatObjectOptions{})
	if err != nil {
		return 0, "", fmt.Errorf("r2 head: %w", err)
	}

	return stat.Size, stat.ContentType, nil
}
