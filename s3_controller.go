package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Hana-ame/azure-go/storage"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// S3Controller handles S3-compatible API requests.
// All storage details (including dedup) are handled by the driver.
type S3Controller struct {
	driver storage.StorageDriver
}

func NewS3Controller(driver storage.StorageDriver) *S3Controller {
	return &S3Controller{driver: driver}
}

// PutObject handles PUT /:bucket/*key
func (s *S3Controller) PutObject(c *gin.Context) {
	bucket := c.Param("bucket")
	key := c.Param("key")
	if key == "" || key == "/" {
		writeS3Error(c, http.StatusBadRequest, "InvalidKey", "Key is required", "/"+bucket)
		return
	}
	path := bucket + key

	contentType := c.GetHeader("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	result, err := s.driver.Put(path, contentType, c.Request.Body)
	if err != nil {
		writeS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), path)
		return
	}

	c.Header("ETag", result.ETag)
	c.Header("x-amz-request-id", uuid.New().String())
	if result.DupPath != "" {
		c.Header("x-amz-dedup-path", result.DupPath)
	}
	c.Status(http.StatusOK)
}

// GetObject handles GET /:bucket/*key
func (s *S3Controller) GetObject(c *gin.Context) {
	bucket := c.Param("bucket")
	key := c.Param("key")
	path := bucket + key

	body, contentLength, contentType, err := s.driver.Get(path)
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found") {
			writeS3Error(c, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.", path)
			return
		}
		writeS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), path)
		return
	}
	defer body.Close()

	// ETag 必须在 header 中返回，先读 body 计算 MD5
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		writeS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), path)
		return
	}

	hash := md5.Sum(bodyBytes)
	etag := `"` + hex.EncodeToString(hash[:]) + `"`

	c.Header("ETag", etag)
	c.Header("x-amz-request-id", uuid.New().String())
	c.DataFromReader(http.StatusOK, contentLength, contentType, bytes.NewReader(bodyBytes), nil)
}

// DeleteObject handles DELETE /:bucket/*key
func (s *S3Controller) DeleteObject(c *gin.Context) {
	bucket := c.Param("bucket")
	key := c.Param("key")
	path := bucket + key

	if err := s.driver.Delete(path); err != nil {
		writeS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), path)
		return
	}

	c.Header("x-amz-request-id", uuid.New().String())
	c.Status(http.StatusNoContent)
}

// HeadObject handles HEAD /:bucket/*key
func (s *S3Controller) HeadObject(c *gin.Context) {
	bucket := c.Param("bucket")
	key := c.Param("key")
	path := bucket + key

	contentLength, contentType, err := s.driver.Head(path)
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found") {
			writeS3Error(c, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.", path)
			return
		}
		writeS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), path)
		return
	}

	c.Header("Content-Length", itoa(contentLength))
	c.Header("Content-Type", contentType)
	c.Header("x-amz-request-id", uuid.New().String())
	c.Status(http.StatusOK)
}

func itoa(n int64) string {
	return fmt.Sprintf("%d", n)
}
