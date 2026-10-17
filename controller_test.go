package main

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMain 初始化全局 agent——hash() 依赖 agent.SALT，测试环境未运行 main()。
func TestMain(m *testing.M) {
	agent = &Agent{SALT: "test-salt"}
	os.Exit(m.Run())
}

// TestTypeByExtension 使用表驱动的方式测试 mime.TypeByExtension 函数
func TestTypeByExtension(t *testing.T) {
	tests := []struct {
		name     string
		ext      string
		expected string
	}{
		{
			name:     "Standard WebP extension",
			ext:      ".webp",
			expected: "image/webp",
		},
		{
			name:     "Empty extension",
			ext:      "",
			expected: "",
		},
		{
			name:     "Uppercase extension",
			ext:      ".WEBP",
			expected: "image/webp",
		},
		{
			name:     "Another known extension",
			ext:      ".json",
			expected: "application/json",
		},
		{
			name:     "Unknown extension",
			ext:      ".notarealext",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := mime.TypeByExtension(tt.ext)
			if r != tt.expected {
				t.Errorf("mime.TypeByExtension(%q) got %q, want %q", tt.ext, r, tt.expected)
			}
		})
	}
}

// TestIsPrivateHost 验证内网/回环地址检测——这些地址绝不能出现在返回给外部客户端的 URL 里。
func TestIsPrivateHost(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		expected bool
	}{
		{"empty", "", true},
		{"localhost", "localhost", true},
		{"loopback 127.0.0.1", "127.0.0.1", true},
		{"internal 127.23.12.17 (reverse-proxy Host seen in prod)", "127.23.12.17", true},
		{"rfc1918 10.x", "10.0.0.5", true},
		{"rfc1918 172.16.x", "172.16.5.5", true},
		{"rfc1918 172.31.x", "172.31.255.1", true},
		{"172.15.x out of rfc1918 range", "172.15.5.5", false},
		{"172.32.x out of rfc1918 range", "172.32.5.5", false},
		{"rfc1918 192.168.x", "192.168.1.1", true},
		{"0.0.0.0", "0.0.0.0", true},
		{"ipv6 loopback", "[::1]", true},
		{"public domain", "upload.moonchan.xyz", false},
		{"public ip", "45.130.22.56", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPrivateHost(tt.host); got != tt.expected {
				t.Errorf("isPrivateHost(%q) = %v, want %v", tt.host, got, tt.expected)
			}
		})
	}
}

// TestPublicBaseURL_PrivateHost 内网 Host → 回落默认公网域（修复回环 IP 的核心路径）
func TestPublicBaseURL_PrivateHost(t *testing.T) {
	os.Unsetenv("PUBLIC_BASE_URL")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/upload", nil)
	c.Request.Host = "127.23.12.17:8080"

	if got := publicBaseURL(c); got != defaultPublicBaseURL {
		t.Errorf("publicBaseURL() = %q, want %q", got, defaultPublicBaseURL)
	}
}

// TestPublicBaseURL_PublicHost 公网 Host → 用请求基址（保持正常部署行为）
func TestPublicBaseURL_PublicHost(t *testing.T) {
	os.Unsetenv("PUBLIC_BASE_URL")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/upload", nil)
	c.Request.Host = "upload.moonchan.xyz"
	c.Request.Header.Set("X-Forwarded-Proto", "https")

	if got := publicBaseURL(c); got != "https://upload.moonchan.xyz" {
		t.Errorf("publicBaseURL() = %q, want %q", got, "https://upload.moonchan.xyz")
	}
}

// TestPublicBaseURL_EnvOverride PUBLIC_BASE_URL 优先级最高（覆盖内网检测）
func TestPublicBaseURL_EnvOverride(t *testing.T) {
	os.Setenv("PUBLIC_BASE_URL", "https://upload.moonchan.xyz")
	defer os.Unsetenv("PUBLIC_BASE_URL")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/upload", nil)
	c.Request.Host = "127.23.12.17:8080"

	if got := publicBaseURL(c); got != "https://upload.moonchan.xyz" {
		t.Errorf("publicBaseURL() = %q, want %q", got, "https://upload.moonchan.xyz")
	}
}

// TestUploadResp_DeleteURLNoLoopback 回归：内网 Host 时 delete_url 不得含回环地址。
func TestUploadResp_DeleteURLNoLoopback(t *testing.T) {
	os.Unsetenv("PUBLIC_BASE_URL")
	os.Unsetenv("R2_PUBLIC_URL")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/upload", nil)
	c.Request.Host = "127.23.12.17:8080"

	uploadResp(c, "1791631702/_up.bin", "r2", nil)

	var resp struct {
		URL       string `json:"url"`
		DeleteURL string `json:"delete_url"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	for _, field := range []string{"url", "delete_url"} {
		val := resp.DeleteURL
		if field == "url" {
			val = resp.URL
		}
		if strings.Contains(val, "127.") || strings.Contains(val, "localhost") || strings.Contains(val, "10.") || strings.Contains(val, "192.168.") {
			t.Errorf("%s contains a private/loopback address: %q", field, val)
		}
	}
	if !strings.HasPrefix(resp.DeleteURL, defaultPublicBaseURL+"/r2/") {
		t.Errorf("delete_url = %q, want prefix %q/r2/", resp.DeleteURL, defaultPublicBaseURL)
	}
}

// TestDestFromRequest_Query query 参数（向后兼容旧消费方）
func TestDestFromRequest_Query(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/upload?dest=r2", nil)
	if got := destFromRequest(c); got != "r2" {
		t.Errorf("destFromRequest() = %q, want %q", got, "r2")
	}
}

// TestDestFromRequest_Path 路径形态 /api/upload/<dest>（无 query 参数）
func TestDestFromRequest_Path(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/upload/local", nil)
	c.Params = gin.Params{{Key: "fn", Value: "/local"}}
	if got := destFromRequest(c); got != "local" {
		t.Errorf("destFromRequest() = %q, want %q", got, "local")
	}
}

// TestDestFromRequest_Empty 无 query 无 path → 空（走默认后端）
func TestDestFromRequest_Empty(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/upload", nil)
	if got := destFromRequest(c); got != "" {
		t.Errorf("destFromRequest() = %q, want empty", got)
	}
}

// TestDefaultDestFromHint 验证 webp/png/jpg/jpeg/gif/bmp 默认走 p.sda1.dev，其余走 r2
func TestDefaultDestFromHint(t *testing.T) {
	cases := []struct {
		filename    string
		contentType string
		want        string
	}{
		{"a.webp", "image/webp", "p.sda1.dev"},
		{"a.png", "image/png", "p.sda1.dev"},
		{"a.jpg", "image/jpeg", "p.sda1.dev"},
		{"a.jpeg", "image/jpeg", "p.sda1.dev"},
		{"a.gif", "image/gif", "p.sda1.dev"},
		{"a.bmp", "image/bmp", "p.sda1.dev"},
		{"", "image/bmp", "p.sda1.dev"},
		{"a.avif", "image/avif", "r2"},
		{"archive.zip", "application/zip", "r2"},
		{"doc.pdf", "application/pdf", "r2"},
		{"", "application/octet-stream", "r2"},
	}
	for _, tc := range cases {
		if got := defaultDestFromHint(tc.filename, tc.contentType); got != tc.want {
			t.Errorf("defaultDestFromHint(%q, %q) = %q, want %q", tc.filename, tc.contentType, got, tc.want)
		}
	}
}

// TestEffectiveDest_AzureIgnored 验证 azure 不再用于上传，退回默认路由规则
func TestEffectiveDest_AzureIgnored(t *testing.T) {
	if got := effectiveDest("azure", "pic.png", "image/png"); got != "p.sda1.dev" {
		t.Errorf("effectiveDest(azure, pic.png) = %q, want p.sda1.dev", got)
	}
	if got := effectiveDest("azure", "pic.avif", "image/avif"); got != "r2" {
		t.Errorf("effectiveDest(azure, pic.avif) = %q, want r2", got)
	}
}
