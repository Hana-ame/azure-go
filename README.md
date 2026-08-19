# Moonchan Upload Server

Go 图片/文件上传服务，支持 R2 (Cloudflare) 和本地文件系统双后端。

## 快速开始

```bash
# 构建
CGO_ENABLED=0 go build -o temp .

# 运行 (本地开发)
AZURE_DISABLE=true LISTEN_ADDR=0.0.0.0:8080 LOCAL_STORAGE_PATH=./data SALT=mysecret ./temp
```

## 配置

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `R2_ACCOUNT_ID` | Cloudflare R2 账户 ID | — |
| `R2_ACCESS_KEY_ID` | R2 Access Key | — |
| `R2_SECRET_ACCESS_KEY` | R2 Secret Key | — |
| `R2_BUCKET_NAME` | R2 存储桶名称 | — |
| `R2_PUBLIC_URL` | R2 公共访问 URL | — |
| `LOCAL_STORAGE_PATH` | 本地存储根目录 | `.` |
| `LOCAL_PUBLIC_URL` | 本地文件公开 URL 前缀 | 自动从请求 Host 推断 |
| `LISTEN_ADDR` | 监听地址 | `:8080` |
| `AZURE_DISABLE` | 禁用 Azure OneDrive (`true`) | — |
| `SALT` | 删除密钥盐值 | — |

配置写入 `.env` 文件自动加载，或通过环境变量传入。

## API

### 上传文件

```bash
# PUT raw body
curl -X PUT 'https://upload.moonchan.xyz/api/upload' \
  -H 'Content-Type: image/jpeg' \
  --data-binary @photo.jpg

# POST multipart (前端友好)
curl -X POST 'https://upload.moonchan.xyz/api/upload' \
  -F 'file=@photo.jpg'
```

### 本地存储

```bash
# 上传到本地文件系统 (非 R2)
curl -X PUT 'https://upload.moonchan.xyz/api/upload?dest=local' \
  -H 'Content-Type: image/jpeg' \
  --data-binary @photo.jpg

# 或通过前端网页
# http://localhost:8080/?dest=local
# localhost 自动使用本地存储 (无需 ?dest=local)
```

### 访问文件

```bash
# 本地存储文件
curl https://upload.moonchan.xyz/local/1700000000/abc123.jpg

# R2 文件
curl https://r2.moonchan.xyz/1700000000/abc123.jpg
```

### 删除文件

```bash
# 使用返回的 delete_url 即可
curl -X GET 'https://upload.moonchan.xyz/local/1700000000/abc123.jpg?delete=<hash>'
```

### S3 兼容接口

```go
// Endpoint: https://upload.moonchan.xyz
// Bucket: 任意 (仅用于路径匹配)
// 支持操作: PUT/GET/DELETE/HEAD
// ?dest=local 同样支持
```

## 响应格式

```json
{
  "id": "<Azure OneDrive item ID; empty for non-Azure backends>",
  "path": "/<ts>/<filename>",
  "url": "https://<访问地址>/<path>",
  "delete_url": "https://<服务地址>/<backend>/<path>?delete=<hash>"
}
```

- `url`: 可直接访问的链接，图片浏览器内联显示
- `delete_url`: 访问即删除（需要 hash 验证）
- `id`: Azure 返回 OneDrive item ID；R2/Local 等非 Azure 后端为空字符串 (保留字段)

## upload.py

```bash
python upload.py photo.jpg           # 手动按任意键上传
python upload.py -a photo.jpg        # 自动上传，无需确认
```

## 架构

```
客户端 → nginx (443) → Go 服务 (8080)
                        ├── R2 (默认)
                        └── 本地文件系统 (?dest=local)
```
