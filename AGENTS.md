# AGENTS.md — Azure Go (onedrive-cf)

## Project State (v26.07.23)

### Architecture
- **Language**: Go 1.23.3, CGO_ENABLED=0 for static builds
- **HTTP**: Gin (v1) — `main.go` starts two Gin servers:
  - `LISTEN_ADDR` (default `:8080`): original API routes + S3-compatible routes
  - `127.26.7.10:8080`: dedicated single-bucket S3 server (`ListenBucket`)
- **HTTP client**: `myfetch.Fetch` wrapper around `http.Client.Do()`
- **Config**: `.env` (auto-loaded via `godotenv/autoload`) + `access_token` file + `client_secrets.txt`

### Authentication
- **Flow**: `client_credentials` (OAuth2 v2.0), server-to-server, no user interaction
- **Grant**: `client_credentials` → scope `https://graph.microsoft.com/.default`
- **Secret**: read from `client_secrets.txt` (plain text file, no trailing newline)
- **Token file**: saved to `access_token` file via `godotenv.Write` after each renewal
- **Renewal**: background goroutine every 20 minutes; retries immediately on failure
- **DO NOT** send `Origin` header — Azure rejects with `AADSTS9002326`
- **No refresh_token** — doesn't exist in `client_credentials` flow
- **User context**: `user_id` field in Agent (e.g. `shami@yumenaka.onmicrosoft.com`)

### Storage Backends

| Backend | Driver | Env Vars | Purpose |
|---------|--------|----------|---------|
| **OneDrive** | `Agent` (azure_drive.go) | tenent_id, client_id, ..., access_token | Legacy read/delete via Graph API |
| **Cloudflare R2** | `r2.Driver` (storage/r2/) | `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET_NAME`, `R2_PUBLIC_URL` | Default upload destination (bucket: `r2-moonchan`, public: `r2.moonchan.xyz`) |
| **Local filesystem** | `localfs.Driver` (storage/local/) | `LOCAL_STORAGE_PATH` (default `.`), `LOCAL_PUBLIC_URL` | `?dest=local` uploads |
| **p.sda1.dev** | `p_sda1_dev.Driver` (storage/p_sda1_dev/) | — (always available) | `?dest=p.sda1.dev` uploads, proxies to p.sda1.dev API |

### API Routes

| Method | Route | Backend | Description |
|--------|-------|---------|-------------|
| PUT | `/api/upload?dest=local` | **R2** or **Local** | Upload with optional `?dest=local` |
| POST | `/api/upload?dest=local` | **R2** or **Local** | Multipart upload with optional `?dest=local` |
| GET | `/api/:id/*fn` | OneDrive | Get file by OneDrive item ID (legacy) |
| DELETE | `/api/:id/:key` | OneDrive | Delete by OneDrive item ID (legacy) |
| GET | `/api/delete/:id/:key` | OneDrive | DeleteWithKey by OneDrive item ID (legacy) |
| GET | `/r2/*path` | R2 | R2 file serve/delete |
| GET | `/local/*path` | Local | Local file serve/delete |
| PUT | `/:bucket/*key?dest=local` | **Azure** or **Local** | S3-compat upload with optional `?dest=local` |
| GET | `/:bucket/*key?dest=local` | **Azure** or **Local** | S3-compat GET |
| DELETE | `/:bucket/*key?dest=local` | **Azure** or **Local** | S3-compat DELETE |
| HEAD | `/:bucket/*key?dest=local` | **Azure** or **Local** | S3-compat HEAD |

### Upload Response Format
```json
{
  "id": "<sha256 hash>",
  "path": "/<ts>/<filename>",
  "url": "https://<PUBLIC_URL>/<path>",
  "delete_url": "https://upload.moonchan.xyz/<backend>/<path>?delete=<hash>"
}
```

### Local Storage (`storage/local/local.go`)
- Implements `storage.StorageDriver` using standard `os` package
- `Put`: MD5-hashes content during write for ETag
- File path: `{LOCAL_STORAGE_PATH}/{ts}/{fn}`
- Served at `/local/{ts}/{fn}`
- Supported via `?dest=local` on upload & S3 routes
- `LOCAL_PUBLIC_URL` env var for public URL prefix (default `https://upload.moonchan.xyz`)

### Build & Deploy
- Build: `CGO_ENABLED=0 go build -o temp` (static binary for VPS)
- VPS: `/root/azure/` directory, run via `nohup ./temp > nohup.out 2>&1 &`
- Config files on VPS:
  - `.env`: tenant_id, client_id, redirect_url, scope, user_id, origin, LISTEN_ADDR, R2_*, LOCAL_*
  - `client_secrets.txt`: Azure Client Secret (plain text)
  - `access_token`: current access token (auto-updated)

### VPS Details
- Host: `vps.moonchan.xyz`
- Access: `root@vps.moonchan.xyz`
- Process: `temp` (binary), no systemd service yet
- Logs: `nohup.out`

### Diagnostics
- `[Init]` — startup info (Version, CWD, file loads, client_secret presence)
- `[Token Loaded]` — access_token at startup
- `[TokenRefresh]` — every renewal attempt (endpoint, params, HTTP status, raw body, result)
- `[WARN]` — renewal failures with error message

### Key Gotchas
1. **Origin header**: never send `Origin` in token requests to Azure
2. **Static build**: VPS has older glibc, must use `CGO_ENABLED=0`
3. **client_secrets.txt**: no newline at end, pure secret value
4. **file rename**: was `access`, now `access_token` (changed in v26.07.10)
5. **Token persistence**: only `access_token` file needed; `refresh_token` file no longer used

### Recent Changes
- R2 bucket switched from `oss-moonchan-xyz` → `r2-moonchan`, public URL → `r2.moonchan.xyz`
- Frontend `index.html` reads `?dest` from URL query and passes to API
- `AZURE_DISABLE=true` local dev flag (skips token renewal, OneDrive handlers return 503)
- `?dest=local` support for `/api/upload` and S3 routes
- Added `storage/local/local.go` — filesystem StorageDriver
- Added `GET /local/*path` for local file serving/deletion
- S3 controller supports `?dest=local` to use local driver
- nginx: single `location /` proxy_pass for all routes (VPS + cloudcone)
- Upload script (`upload.py`) uses `data.url` from response
- 1s sleep delay added to RenewToken failure loop
- Version bumped to v26.07.22
- Added `storage/p_sda1_dev/` driver — proxies uploads via p.sda1.dev API
- `?dest=p.sda1.dev` supported on `/api/upload` routes; returns `path=/p.sda1.dev/...` `url=https://p.sda1.dev/...`
- Added `GET /p.sda1.dev/*path` for proxied file serving
- Added `Path` field to `storage.PutResult` for externally-assigned paths
- Version bumped to v26.07.23

### Next Steps (suggested)
- [ ] Set up systemd service for auto-restart
