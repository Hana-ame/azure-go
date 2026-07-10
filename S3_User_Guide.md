# S3 兼容层使用说明

**适用版本:** v26.07.10+

---

## 概述

`azure-go` 提供了兼容 Amazon S3 API 的存储接口，底层对接 Azure Drive。支持内容去重（相同内容只存一份）和路径映射（逻辑路径 $\to$ Azure 资源 ID，去重后路径不失效）。

---

## 一、连接方式

### 方式 A：多 Bucket API（默认）

主服务（`:8080`）提供多 bucket 路由：

```
PUT /{bucket}/{key}         上传文件
GET /{bucket}/{key}         下载文件
HEAD /{bucket}/{key}        获取元数据（Content-Length、Content-Type）
DELETE /{bucket}/{key}      删除文件
```

### 方式 B：单 Bucket 服务器（独立端口）

`127.26.7.10:8080` 运行一个固定 bucket 为 `mybucket` 的服务器，路径即为 key：

```
PUT /path/to/file           上传到 mybucket/path/to/file
GET /path/to/file           获取
HEAD /path/to/file          元数据
DELETE /path/to/file        删除
```

适合将整个端口绑定到一个域名/Nginx 的场景。

---

## 二、认证

单 Bucket 服务器支持 **Bearer Token 鉴权**。

设置环境变量 `S3_AUTH_TOKEN` 后，所有请求必须带 token：

```bash
# 设置 token
export S3_AUTH_TOKEN="your-secret-token"

# 请求时必须带
curl -H "Authorization: Bearer your-secret-token" http://127.26.7.10:8080/myfile.txt
```

如果 `S3_AUTH_TOKEN` 为空，则**关闭鉴权**，任何客户端均可访问。

---

## 三、使用示例（curl）

### 上传文件

```bash
# 多 bucket 模式：上传到 bucket "images"，key 为 "photo.jpg"
curl -X PUT http://host:8080/images/photo.jpg \
  -H "Content-Type: image/jpeg" \
  --data-binary @photo.jpg

# 单 bucket 模式：直接上传到 mybucket
curl -X PUT http://127.26.7.10:8080/photo.jpg \
  -H "Content-Type: image/jpeg" \
  --data-binary @photo.jpg
```

响应头包含 `ETag`（内容 MD5）和 `x-amz-request-id`。
如果触发了去重，会额外返回 `x-amz-dedup-path`。

### 下载文件

```bash
# 方式 A：指定 bucket
curl http://host:8080/images/photo.jpg -o photo.jpg

# 方式 B：单 bucket（路径即为 key）
curl http://127.26.7.10:8080/photo.jpg -o photo.jpg
```

### 获取元数据

```bash
curl -I http://host:8080/images/photo.jpg
# Content-Length: 12345
# Content-Type: image/jpeg
```

### 删除文件

```bash
curl -X DELETE http://host:8080/images/photo.jpg
```

成功返回 `204 No Content`。

---

## 四、关键特性（透明自动运行）

以下特性无需用户配置，系统自动处理：

### 1. 内容去重（Deduplication）
同一份内容上传多次，Azure 上**只存一份**。
```bash
# 上传两次相同内容到不同路径
echo "hello" | curl -X PUT --data-binary @- http://host:8080/bucket/a.txt
echo "hello" | curl -X PUT --data-binary @- http://host:8080/bucket/b.txt
# b.txt 不会被真正存储，而是指向 a.txt 的物理文件。
# 两个路径都可以正常下载，互不影响。
```

### 2. 路径映射（Path-to-ID）
系统在本地 `paths.db` 中维护 S3 路径 $\to$ Azure 资源 ID 的映射。即使底层文件因去重被移动或删除，访问路径仍然有效。**该过程完全无感。**

- 上传时自动记录映射。
- 下载/删除时优先使用 Azure 资源 ID 直接操作，速度更快。

### 3. Token 自动刷新
系统后台每 20 分钟自动刷新 Azure Access Token。无需人工干预。

---

## 五、配置说明

### 环境变量

| 变量 | 作用 | 默认值 |
| :--- | :--- | :--- |
| `LISTEN_ADDR` | 主服务监听地址 | `:8080` |
| `S3_AUTH_TOKEN` | 单 Bucket 服务器 Bearer Token | 空（无认证） |
| `tenent_id` | Azure 租户 ID | 必填 |
| `client_id` | Azure 应用 ID | 必填 |
| `client_secret` | Azure 应用密钥 | 必填 |
| `SCOPE` | OAuth 作用域 | 必填 |

### 本地数据文件

| 文件 | 用途 | 说明 |
| :--- | :--- | :--- |
| `hashes.db` | 内容去重索引（MD5 $\to$ 路径） | 自动创建，保留历史 |
| `paths.db` | 路径 $\to$ 资源 ID 映射 | 自动创建，保留历史 |
| `refresh_token` | 持久化的 Refresh Token | 自动更新 |

---

## 六、日志与调试

启动日志中包含诊断信息：

```
[Init] Version: v26.07.10
[Init] Current Working Directory: /home/user/azure
[Init] Successfully loaded from file: refresh_token
[Token Loaded] access_token:
<token value>
[Init] Agent Config: tenent_id=xxx, client_id=xxx, scope=xxx
[Token Renewed] access_token:
OLD: <old token>
NEW: <new token>
```

### 排查环境问题（Crontab 场景）
如果自动启动失败但手动启动正常，查看 `nohup.out` 中的：
1. **`Current Working Directory`** — 是否在正确的目录
2. **`Successfully loaded from file`** — `refresh_token` 和 `access` 文件是否被正确加载
3. **`Token Loaded`** — Token 值是否为空
