package p_sda1_dev

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Hana-ame/azure-go/storage"
)

type uploadResponse struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Size int64  `json:"size"`
		Path string `json:"path"`
		URL  string `json:"url"`
	} `json:"data"`
}

type Driver struct {
	baseURL string
	apiURL  string
	client  *http.Client
}

func New() *Driver {
	return &Driver{
		baseURL: "https://p.sda1.dev",
		apiURL:  "https://p.sda1.dev/api/v1/upload_external_noform",
		client:  newHTTPClient(),
	}
}

func (d *Driver) Put(path, contentType string, body io.Reader) (*storage.PutResult, error) {
	parts := strings.Split(strings.TrimRight(path, "/"), "/")
	filename := parts[len(parts)-1]
	if filename == "" {
		filename = "file"
	}

	// 照 azure/r2 driver（storage/azure/driver.go:31-35）：流式透传 body，不缓冲
	// 整文件进内存。io.TeeReader 边传边算 md5；tee 作为 http.NewRequest 的 body
	// 且不设 ContentLength → Go HTTP client 走 chunked transfer encoding（stream）。
	// upload_external_noform 接 raw body（端点名 noform = no multipart form），
	// filename 走 query；不包 multipart（旧实现包 multipart 致 p.sda1.dev 存信封、
	// 回读渲染坏，2026-10-17 实测 PUT raw/POST multipart 回读均为 multipart 信封，
	// boundary 为 Go 侧随机生成，证明信封由本 driver 产生）。
	hash := md5.New()
	tee := io.TeeReader(body, hash)

	req, err := http.NewRequest("POST", d.apiURL+"?filename="+filename, tee)
	if err != nil {
		return nil, fmt.Errorf("p.sda1.dev create req: %w", err)
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filename))

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("p.sda1.dev post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("p.sda1.dev api responded %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var apiResp uploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("p.sda1.dev decode: %w", err)
	}
	if !apiResp.Success {
		return nil, fmt.Errorf("p.sda1.dev upload failed: %s", apiResp.Message)
	}

	storagePath := strings.TrimPrefix(apiResp.Data.Path, "/")
	md5hex := hex.EncodeToString(hash.Sum(nil))
	etag := `"` + md5hex + `"`
	return &storage.PutResult{ID: storagePath, Path: storagePath, ETag: etag}, nil
}

func (d *Driver) Get(path string) (io.ReadCloser, int64, string, error) {
	url := d.baseURL + "/" + strings.TrimPrefix(path, "/")
	resp, err := d.client.Get(url)
	if err != nil {
		return nil, 0, "", fmt.Errorf("p.sda1.dev get: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, "", fmt.Errorf("p.sda1.dev get: %s", resp.Status)
	}
	return resp.Body, resp.ContentLength, resp.Header.Get("Content-Type"), nil
}

func (d *Driver) Delete(path string) error {
	return fmt.Errorf("p.sda1.dev delete not supported")
}

func (d *Driver) Head(path string) (int64, string, error) {
	url := d.baseURL + "/" + strings.TrimPrefix(path, "/")
	resp, err := d.client.Head(url)
	if err != nil {
		return 0, "", fmt.Errorf("p.sda1.dev head: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("p.sda1.dev head: %s", resp.Status)
	}
	return resp.ContentLength, resp.Header.Get("Content-Type"), nil
}
