# azure-go API 调用链文档

## 原有 API

### 1. `PUT /api/upload`

```
HTTP PUT /api/upload
│
└─ controller.Upload(c *gin.Context)
   ├─ c.GetHeader("Content-Type")          → 读请求头
   ├─ c.GetHeader("Content-Length")        → 读长度
   ├─ c.Request.Body                       → 拿 body reader
   │
   └─ agent.Upload(ContentType, ContentLength, body)
      ├─ 拼接 endpoint:
      │     graph.microsoft.com/v1.0/me/drive/root:/img/{ts}-{uuid}{ext}:/content
      │     (ext 由 contentTypeToExtend 从 MIME 推导)
      │
      ├─ myfetch.Fetch(PUT, endpoint, headers{Auth, CT, CL}, body)
      │  └─ myfetch.NewRequest()           → 构造 *http.Request
      │  └─ myfetch.FetchWithRequest()     → Client().Do(req)
      │     └─ myfetch.Client()            → 返回 DefaultClient (被 main 覆写过)
      │
      └─ myfetch.ResponseToJson(resp)      → 解析 JSON → *orderedmap.OrderedMap
         → 返回 {id, name, ...}

   返回: c.JSON(200, {id, key})            (key = SHA256(id+SALT)[:16])
```

### 2. `POST /api/upload` (大文件会话)

```
HTTP POST /api/upload
│
└─ controller.CreateUploadSession(c)
   └─ agent.CreateUploadSession(ContentType, nil)
      ├─ endpoint: .../root:/large/{ts}-{uuid}{ext}:/createUploadSession
      └─ myfetch.Fetch(POST, endpoint, headers{Auth}, nil)
         → 返回 uploadUrl (客户端直接分片上传)
```

### 3. `GET /api/:id/*fn`

```
HTTP GET /api/01LLW.../photo.jpg
│
└─ controller.Get(c)
   ├─ id = c.Param("id")                  → "01LLW..."
   ├─ fn = c.Param("fn")                  → "/photo.jpg"
   ├─ ext = filepath.Ext(fn)              → ".jpg"
   │
   ├─ Deleted.Get(id)                     → 查 LRU 删除缓存
   │  └─ 命中 + 未过期? → 302 → os.Getenv("default")  (兜底图)
   │
   └─ agent.Get(id, fn)                   → Azure: GET /items/{id}/content
      ├─ myfetch.Fetch(GET, endpoint, headers{Auth}, nil)
      └─ 返回 (body io.ReadCloser, contentLength, contentType)
         │
         ├─ file.Close()  defer
         ├─ 检查 contentType == "application/json; odata..."  → 软删除 → 500
         │
         └─ c.DataFromReader(200, contentLength, finalContentType, file, ...)
            finalContentType = tools.Or(
                tools.Ternary(ext==".webp", "image/webp", ""),   ← 修正 webp
                mime.TypeByExtension(ext),                        ← 标准 MIME
                contentType)                                      ← 上游返回的
```

### 4. `DELETE /api/:id/:key`

```
HTTP DELETE /api/01LLW.../a1b2c3d4
│
└─ controller.Delete(c)
   ├─ id = c.Param("id")
   │
   └─ agent.Delete(id)                    → Azure: DELETE /items/{id}
      └─ myfetch.Fetch(DELETE, endpoint, headers{Auth}, nil)
         ├─ io.EOF? → 返回 "ok"           (Azure 204 表现为 EOF)
         └─ 其他错误 → 500
            │
            └─ Deleted.Put(id, tools.NewTimeStamp())   ← 写入 LRU, 标记 30min 内不穿透
```

### 5. `GET /api/delete/:id/:key`

```
HTTP GET /api/delete/01LLW.../a1b2c3d4
│
└─ controller.DeleteWithKey(c)
   ├─ id, key = params
   ├─ hash(id) != key?  → 401            (防未授权删除)
   │  hash = SHA256(id + agent.SALT)[:16]
   │
   ├─ Deleted.Get(id) 命中? → 200 "not found"
   │
   └─ agent.Delete(id)                   → 同上
      └─ Deleted.Put(id, NewTimeStamp())
```

---

## S3 兼容 API

### 6. `PUT /:bucket/*key`

```
HTTP PUT /mybucket/photos/cat.jpg
│
└─ s3_controller.PutObject(c)
   ├─ bucket = "mybucket", key = "/photos/cat.jpg"
   ├─ path = "mybucket/photos/cat.jpg"
   ├─ contentType = c.GetHeader("Content-Type") || "application/octet-stream"
   │
   └─ s3.driver.Put(path, contentType, c.Request.Body)
      │
      └─ 进入 driver 实现，以 AzureDriver 为例:
         │
         ├─ md5.New()                          → 创建 hash 计算器
         ├─ io.TeeReader(body, hash)           → 流式 tee: 写到 Azure 的同时算 MD5
         │
         ├─ agent.Put(path, contentType, tee)  → Azure: PUT /root:/{path}:/content
         │  └─ myfetch.Fetch(...) → *orderedmap.OrderedMap → 提取 id
         │
         ├─ md5hex = hex(hash.Sum(nil))
         ├─ etag = `"` + md5hex + `"`
         │
         ├─ hashStore.Lookup(md5hex)           → SQLite: SELECT path FROM hashes WHERE md5=?
         │  └─ 命中:
         │     ├─ agent.DeleteByPath(path)      → 删刚上传的重复文件
         │     └─ return DupPath=existingPath   → 标记去重
         │  └─ 未命中:
         │     └─ hashStore.Store(md5hex, path) → INSERT INTO hashes
         │
         └─ return &PutResult{ID, ETag, DupPath}

   返回: 200 + ETag: "abc123" [+ x-amz-dedup-path (如果去重)]
```

### 7. `GET /:bucket/*key`

```
HTTP GET /mybucket/photos/cat.jpg
│
└─ s3_controller.GetObject(c)
   ├─ path = "mybucket/photos/cat.jpg"
   │
   └─ s3.driver.Get(path)
      └─ AzureDriver:
         └─ agent.GetByPath(path)               → Azure: GET /root:/{path}:/content
            └─ myfetch.Fetch(GET, ...) → (body, contentLength, contentType)
      │
      ├─ 404? → S3 XML: <Error><Code>NoSuchKey</Code>...
      │
      ├─ io.ReadAll(body)                       → 读全 body (需要先算 ETag)
      ├─ md5.Sum(bodyBytes)                     → 计算 MD5
      ├─ etag = `"` + hex(hash) + `"`
      │
      └─ c.DataFromReader(200, contentLength, contentType, bytes.NewReader(bodyBytes))

   返回: 200 + body + ETag
```

### 8. `DELETE /:bucket/*key`

```
HTTP DELETE /mybucket/photos/cat.jpg
│
└─ s3_controller.DeleteObject(c)
   ├─ path = "mybucket/photos/cat.jpg"
   │
   └─ s3.driver.Delete(path)
      └─ AzureDriver:
         └─ agent.DeleteByPath(path)            → Azure: DELETE /root:/{path}
            └─ myfetch.Fetch(DELETE, ...)

   返回: 204 No Content
```

### 9. `HEAD /:bucket/*key`

```
HTTP HEAD /mybucket/photos/cat.jpg
│
└─ s3_controller.HeadObject(c)
   ├─ path = "mybucket/photos/cat.jpg"
   │
   └─ s3.driver.Head(path)
      └─ AzureDriver:
         └─ agent.HeadByPath(path)              → Azure: HEAD /root:/{path}:/content
            └─ myfetch.Fetch(HEAD, ...) → (contentLength, contentType)

   返回: 200 + Content-Length + Content-Type (无 body)
```

---

## 守护协程

```
go func() { for {                               ← 每 20 分钟
    for err := agent.RenewToken(); err != nil; ... {
        debug.W("renew", err)                   ← 重试直到成功
    }
    time.Sleep(20 * time.Minute)
}}()
│
└─ agent.RenewToken()
   ├─ endpoint: login.microsoftonline.com/{tenant}/oauth2/v2.0/token
   ├─ body: grant_type=refresh_token + client_id + refresh_token + client_secret
   ├─ myfetch.Fetch(POST, ...)
   ├─ myfetch.ResponseToJson(resp) → 提取 access_token, refresh_token, expires_in
   ├─ agt.access_token = new_token
   ├─ agt.refresh_token = new_refresh
   ├─ agt.expires_time = now + expires_in
   ├─ godotenv.Write("refresh_token", "access_token")  → 写磁盘持久化
   └─ tools.WriteJSONToFile("last_result.json", result)
```

---

## 架构分层

```
HTTP Request
  │
  ├─ /api/*           → Gin Handler (controller.go)
  │                     └─ Agent (azure_drive.go)
  │                        └─ myfetch (HTTP client)
  │                           └─ Microsoft Graph API
  │
  └─ /:bucket/*key    → S3Controller (s3_controller.go)
                        └─ storage.StorageDriver (接口)
                           ├─ AzureDriver (driver.go)
                           │  ├─ Agent (azure_drive.go)
                           │  └─ HashStore (hash_store.go → SQLite)
                           │
                           └─ LocalDriver (storage/local/local.go)
                              └─ 本地文件系统
```
