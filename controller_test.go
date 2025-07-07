package main

import (
	"mime"
	"testing"
)

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
