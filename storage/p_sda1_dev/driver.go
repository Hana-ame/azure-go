package p_sda1_dev

import (
	"bytes"
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

	// p.sda1.dev 的 upload_external_noform 端点接 raw body（端点名 noform = no
	// multipart form），filename 走 query。旧实现把 body 包成 multipart 再发，导致
	// p.sda1.dev 把整段 multipart 信封当对象存，回读即信封而非干净图片（浏览器
	// <img> 渲染坏）。实测（2026-10-17）：PUT raw body 与 POST multipart 回读均为
	// multipart 信封，boundary 为 Go 侧随机生成——证明信封由本 driver 产生，与前端
	// 上传方式无关。故直接发 raw body，不再包 multipart。
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("p.sda1.dev read body: %w", err)
	}

	req, err := http.NewRequest("POST", d.apiURL+"?filename="+filename, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("p.sda1.dev create req: %w", err)
	}
	req.ContentLength = int64(len(data))
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
	return &storage.PutResult{ID: storagePath, Path: storagePath}, nil
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
