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
	// 定义测试用例的结构体
	// 每个测试用例包含：一个描述性名称、输入值和期望的输出值
	tests := []struct {
		name     string // 测试用例的名称
		ext      string // 输入的扩展名
		expected string // 期望返回的 MIME 类型
	}{
		{
			name:     "Standard WebP extension",
			ext:      ".webp",
			expected: "image/webp",
		},
		{
			name:     "Empty extension", // 修正后的测试：空扩展名应返回空 MIME 类型
			ext:      "",
			expected: "",
		},
		// {
		// 	name:     "Extension without leading dot", // 增加一个边界情况测试
		// 	ext:      "webp",
		// 	expected: "image/webp",
		// },
		{
			name:     "Uppercase extension", // 增加大小写不敏感测试
			ext:      ".WEBP",
			expected: "image/webp",
		},
		{
			name:     "Another known extension", // 增加其他类型的测试
			ext:      ".json",
			expected: "application/json",
		},
		{
			name:     "Unknown extension", // 增加未知扩展名的测试
			ext:      ".notarealext",
			expected: "",
		},
	}

	// 遍历所有测试用例
	for _, tt := range tests {
		// t.Run 会为每个测试用例创建一个子测试
		// 这使得在测试失败时，输出结果更加清晰
		t.Run(tt.name, func(t *testing.T) {
			// 执行被测试的函数
			r := mime.TypeByExtension(tt.ext)

			// 检查实际结果是否与期望结果相符
			if r != tt.expected {
				// 如果不符，使用 t.Errorf 报告错误，它会标记测试失败并输出详细信息
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
// 修复前（requestBaseURL）此测试失败；修复后（publicBaseURL）通过。
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
	t.Logf("delete_url = %q", resp.DeleteURL)
	t.Logf("url = %q (R2_PUBLIC_URL unset, falls back to public base)", resp.URL)
}
