# AGENTS.md — Azure Go (onedrive-cf)

## Project State (v26.10.16)

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
| **OneDrive** | `Agent` (azure_drive.go) | tenent_id, client_id, ..., access_token | `?dest=azure` uploads; legacy read/delete via Graph API. **Never the dest-less default** — the OneDrive quota is exceeded, so a dest-less upload must not land here |
| **Cloudflare R2** | `r2.Driver` (storage/r2/) | `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET_NAME`, `R2_PUBLIC_URL` | `?dest=r2` uploads, and the dest-less default for anything that is not webp/jpg/jpeg/png/gif (bucket: `r2-moonchan`, public: `r2.moonchan.xyz`) |
| **Local filesystem** | `localfs.Driver` (storage/local/) | `LOCAL_STORAGE_PATH` (default `.`), `LOCAL_PUBLIC_URL` | `?dest=local` uploads |
| **p.sda1.dev** | `p_sda1_dev.Driver` (storage/p_sda1_dev/) | — (always available) | `?dest=p.sda1.dev` uploads, and the dest-less default for webp/jpg/jpeg/png/gif. Proxy to p.sda1.dev API |

### API Routes

| Method | Route | Backend | Description |
|--------|-------|---------|-------------|
| PUT | `/api/upload` | **R2**, **Local**, **Azure**, **p.sda1.dev** | Upload, backend via `?dest=` (r2/local/azure/p.sda1.dev) |
| POST | `/api/upload` | **R2**, **Local**, **Azure**, **p.sda1.dev** | Multipart upload, backend via `?dest=` |
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
  "id": "<Azure OneDrive item ID; empty for non-Azure backends>",
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
- `?dest=azure` restored on `/api/upload` — uploads to OneDrive via global `azureDriver` (reused by S3 + bucket server)
- Azure upload response: `id` = OneDrive item ID, `path=/api/<id>`, `delete_url=/api/delete/<id>/<hash(id)>`
- `uploadResp` now takes `result *storage.PutResult` for backend-assigned IDs
- Azure upload restored to legacy `Agent.Upload` path: `img/{ts}-{uuid}{ext}`, no `{ts}/` folder
- Unified upload response: `id` reserved for Azure; R2/local/p.sda1.dev use empty `id`
- Version bumped to v26.08.19
- Version bumped to v26.10.14: Added GIF magic-byte detection to ValidateImage; removed China IP check requirement from psda1proxy; added README caution forbidding non-image uploads to PSDA1.
- Version bumped to v26.10.16: `storage/p_sda1_dev` now uses a dedicated IPv4-only `http.Client` (`newHTTPClient()` in `client.go`: inline `tcp4` `DialContext` + `Proxy: nil`) instead of `http.DefaultClient`. Fixes Cloudflare 403 "error code: 1106" on dual-homed hosts, where RFC 6724 address sorting made `DefaultClient` dial the AAAA record first. Transport is inlined rather than imported from `psda1proxy` to avoid an import cycle (`psda1proxy/driver.go` already imports `p_sda1_dev`); the two copies must be kept in sync. Covers `TestNewHTTPClient_ReachesIPv4Server`, `TestNewHTTPClient_CannotReachIPv6OnlyServer`, `TestNew_ConfirmsConfig`.
  - Note: tag v26.10.15 was withdrawn. It was cut from a tree that had lost all 4 v26.10.14 commits (GIF support, China-IP-check removal, README caution, version bump) — a force push had been validated against a stale local `origin/main` ref instead of a freshly fetched one. v26.10.16 is the rebased cut that restores them.
- Version bumped to v26.10.20: `p_sda1_dev` now stores clean image bytes and repairs the objects written before the fix.
  - Root cause (`336675c`): `storage/p_sda1_dev` `Put` used to wrap the body in a **fresh** multipart envelope (`multipart.NewWriter` + `CreateFormFile("image", ...)`) before POSTing. `upload_external_noform` stores the request body verbatim (the name means "no form"), so it stored the envelope as the object. Read-back returned `--<boundary>\r\nContent-Disposition: form-data; name="image"; filename=...` with the real PNG at offset ~174, while the `Content-Type` header — assigned by Cloudflare from the URL extension — claimed `image/png`. Hence `<img>` rendered broken. Proven by measurement, not inference: the boundary is Go-side random and the field name is `"image"` exactly as `CreateFormFile` was called, so the envelope originates here and is independent of the frontend upload method.
  - `1b1c3c8`: `Put` now streams the body through `io.TeeReader` with no `ContentLength` (chunked transfer) and returns an MD5 `ETag`, mirroring `storage/azure/driver.go` / `storage/r2/driver.go`. No in-memory buffering, so the cloudcone 1 GB OOM risk from `pit-upload-moonchan-azure-oom-502` does not apply.
  - `e4d7a86`: write-side defence-in-depth plus the read-side repair, all in new `storage/p_sda1_dev/envelope.go`.
    - `extractBody` — if a caller still hands `Put` a multipart body, the file part is located (filename-bearing part > a part named `"image"` > first non-empty part) and only its bytes are sent. Non-multipart bodies stream through untouched, so the chunked path is unchanged. It cannot fire through the normal routes: `CreateUploadSession` already extracts `FormFile("file")`, and `psda1proxy.ValidateImage` rejects a raw multipart body upstream of `Put`. Guard for direct driver callers only.
    - `StripEnvelope` / `stripEnvelopeIfPresent` — repairs the **legacy corpus**. Those objects hold the envelope and `p.sda1.dev` has **no delete API**, so they cannot be deleted or re-uploaded over; the only repair is to strip on the way out. Applied in `Driver.Get` and in `psda1proxy.ProxyHandler.Handle`.
    - Only two bytes are read to decide, so every object the fixed driver writes (image magic bytes first) still streams with no buffering; a body that really begins with `--` is read in full because the boundary has to be parsed.
    - Envelopes are recognised by their **first line** only (`--<boundary>\r\n`), never by slicing on `\r\n--`, because binary image data may itself contain that sequence. Decoding uses the standard library `multipart.Reader`, not hand parsing.
  - `98bcf9f`: read-path coverage. `storage/p_sda1_dev` gained 22 tests (`driver_test.go`; the package previously had only the 3 client tests) — `extractBody` x8, `normaliseContentType` x1 table, `StripEnvelope` x6, `Put` x4 (asserts the bytes that actually leave the process, the `filename` query param, and the `ETag`), `Get` x3. Plus `TestProxyHandler_StripsLegacyEnvelope` and `TestProxyHandler_DoesNotStripCleanBody` in `psda1proxy/proxy_test.go`.
  - `b9d4817` (concurrent, not part of this change set): the upload response `url` now points at `/p.sda1.dev/<path>` instead of `/api/p.sda1.dev/<path>`, i.e. at `Driver.Get` rather than the `psda1proxy` error-caching route — which is why the strip lives in `Driver.Get`, not only in the proxy.
  - **Latent bug left alone, out of scope**: `d.apiURL+"?filename="+filename` does not URL-escape the filename, so a name with a space or non-ASCII characters produces a malformed query.

### Next Steps (suggested)
- [ ] Set up systemd service for auto-restart
