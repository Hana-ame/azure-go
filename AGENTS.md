# AGENTS.md — Azure Go (onedrive-cf)

## Project State (v26.07.10)

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

### API Endpoints (Graph)
All endpoints use `/users/{user_id}/drive/...` instead of `/me/drive/...`:

| Method | Function | Path |
|--------|----------|------|
| PUT | Upload | `/users/{user_id}/drive/root:/img/{ts}-{uuid}{ext}:/content` |
| GET | Get | `/users/{user_id}/drive/items/{id}/content` |
| DELETE | Delete | `/users/{user_id}/drive/items/{id}` |
| POST | CreateUploadSession | `/users/{user_id}/drive/root:/large/{ts}-{uuid}{ext}:/createUploadSession` |
| PUT | Put (S3) | `/users/{user_id}/drive/root:/{path}:/content` |
| GET | GetByPath | `/users/{user_id}/drive/root:/{path}:/content` |
| HEAD | HeadByID | `/users/{user_id}/drive/items/{id}/content` |
| HEAD | HeadByPath | `/users/{user_id}/drive/root:/{path}:/content` |
| DELETE | DeleteByPath | `/users/{user_id}/drive/root:/{path}` |

### S3 Compatibility
- `PathStore` interface in `storage/driver.go`; `SQLitePathStore` in `hash_store.go`
- `AzureDriver` wraps `Agent` with `hashStore` + `pathStore`
- `S3Controller` in `s3_controller.go`: PutObject/GetObject/DeleteObject/HeadObject
- Bucket server in `s3_bucket_server.go`: `ListenBucket(addr, bucket, driver, authToken)`
- Bearer auth via `S3_AUTH_TOKEN` env var; none if empty

### Build & Deploy
- Build: `CGO_ENABLED=0 go build -o temp` (static binary for VPS)
- VPS: `/root/azure/` directory, run via `nohup ./temp > nohup.out 2>&1 &`
- Config files on VPS:
  - `.env`: tenant_id, client_id, redirect_url, scope, user_id, origin, LISTEN_ADDR, etc.
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
- Replaced `refresh_token` flow with `client_credentials`
- Added `client_secrets.txt` reading
- Changed all `/me/drive` endpoints to `/users/{user_id}/drive`
- Renamed `access` → `access_token` save/load file
- Removed `Origin` header from token requests

### Next Steps (suggested)
- [ ] Add retry delay in RenewToken failure loop (currently hammers Azure on error)
- [ ] Set up systemd service for auto-restart
- [ ] Set `GIN_MODE=release` in production `.env`
