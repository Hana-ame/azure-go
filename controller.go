package main

import (
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tools "github.com/Hana-ame/azure-go/Tools"
	"github.com/Hana-ame/azure-go/Tools/orderedmap"
	"github.com/Hana-ame/azure-go/storage"
	localfs "github.com/Hana-ame/azure-go/storage/local"
	p_sda1_dev "github.com/Hana-ame/azure-go/storage/p_sda1_dev"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var localDriver *localfs.Driver
var pSda1DevDriver *p_sda1_dev.Driver
var azureDriver *AzureDriver
var azureDisabled bool

//go:embed static/*
var embeddedStatic embed.FS

func requestBaseURL(c *gin.Context) string {
	host := c.Request.Host
	scheme := "http"
	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + host
}

// defaultPublicBaseURL is used when the request Host resolves to a private or
// loopback address (reverse proxy forwarding an internal Host) and no
// PUBLIC_BASE_URL is configured. Generated links must point at a host the
// client can actually reach, not the proxy's internal address.
const defaultPublicBaseURL = "https://upload.moonchan.xyz"

// publicBaseURL returns the externally-reachable base URL for generated
// response links (url, delete_url). Precedence:
//  1. PUBLIC_BASE_URL env (explicit public domain, e.g. https://upload.moonchan.xyz)
//  2. defaultPublicBaseURL when the request Host is a private/loopback address
//  3. requestBaseURL(c) otherwise (keeps local dev without env working)
func publicBaseURL(c *gin.Context) string {
	if base := os.Getenv("PUBLIC_BASE_URL"); base != "" {
		return strings.TrimRight(base, "/")
	}
	host := c.Request.Host
	hostOnly := strings.SplitN(host, ":", 2)[0]
	if isPrivateHost(hostOnly) {
		return defaultPublicBaseURL
	}
	return requestBaseURL(c)
}

// isPrivateHost reports whether host is a loopback or RFC1918 private address,
// which must never appear in a URL handed to an external client.
func isPrivateHost(host string) bool {
	switch host {
	case "", "localhost", "0.0.0.0", "::1", "[::1]":
		return true
	}
	if strings.HasPrefix(host, "127.") || strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "192.168.") {
		return true
	}
	if strings.HasPrefix(host, "172.") {
		if parts := strings.Split(host, "."); len(parts) >= 2 {
			if n, err := strconv.Atoi(parts[1]); err == nil && n >= 16 && n <= 31 {
				return true
			}
		}
	}
	return false
}

func uploadResp(c *gin.Context, path string, dest string, result *storage.PutResult) {
	h := hash(path)
	o := orderedmap.New()
	o.Set("id", "")
	o.Set("path", "/"+path)

	if dest == "p.sda1.dev" {
		o.Set("path", "/p.sda1.dev/"+path)
		reqBase := publicBaseURL(c)
		o.Set("url", reqBase+"/p.sda1.dev/"+path)
		o.Set("delete_url", "https://p.sda1.dev/"+path)
		c.JSON(http.StatusOK, o)
		return
	}

	reqBase := publicBaseURL(c)

	if dest == "local" {
		publicURL := os.Getenv("LOCAL_PUBLIC_URL")
		if publicURL == "" {
			publicURL = reqBase
		}
		o.Set("url", publicURL+"/local/"+path)
		o.Set("delete_url", reqBase+"/local/"+path+"?delete="+h)
	} else {
		publicURL := os.Getenv("R2_PUBLIC_URL")
		if publicURL == "" {
			publicURL = reqBase
		}
		o.Set("url", publicURL+"/"+path)
		o.Set("delete_url", reqBase+"/r2/"+path+"?delete="+h)
	}
	c.JSON(http.StatusOK, o)
}

// Image types a dest-less upload sends to p.sda1.dev (webp, png, jpg/jpeg, gif, bmp),
// matched by file extension first and content type second. Everything else —
// avif, non-images, unknown or missing types — goes to r2.
var pSda1ImageExts = map[string]bool{
	".webp": true,
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".bmp":  true,
}

var pSda1ImageTypes = map[string]bool{
	"image/webp":     true,
	"image/png":      true,
	"image/jpeg":     true,
	"image/jpg":      true,
	"image/gif":      true,
	"image/bmp":      true,
	"image/x-bmp":    true,
	"image/x-ms-bmp": true,
}

// defaultDestFromHint picks the backend for an upload that carries no explicit
// dest. Default is p.sda1.dev for webp/png/jpg/jpeg/gif/bmp; all other formats
// fall back to r2.
func defaultDestFromHint(filename, contentType string) string {
	if ext := strings.ToLower(filepath.Ext(filename)); ext != "" {
		if pSda1ImageExts[ext] {
			return "p.sda1.dev"
		}
		return "r2"
	}
	if ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])); pSda1ImageTypes[ct] {
		return "p.sda1.dev"
	}
	return "r2"
}

// effectiveDest resolves the actual upload backend for a request.
// Azure is no longer used for uploads; if dest is empty or "azure", the backend
// is chosen from the file type via defaultDestFromHint.
func effectiveDest(dest, filename, contentType string) string {
	if dest != "" && dest != "azure" {
		return dest
	}
	return defaultDestFromHint(filename, contentType)
}

var validUploadDests = map[string]bool{
	"p.sda1.dev": true,
	"r2":         true,
	"local":      true,
}

// parseUploadPath splits Param("fn") into an optional explicit destination
// ("local", "r2", "p.sda1.dev") and an optional filename.
func parseUploadPath(rawFn string) (dest, filename string) {
	trimmed := strings.Trim(rawFn, "/")
	if trimmed == "" {
		return "", ""
	}
	if validUploadDests[trimmed] {
		return trimmed, ""
	}
	if parts := strings.SplitN(trimmed, "/", 2); len(parts) == 2 && validUploadDests[parts[0]] {
		return parts[0], filepath.Base(parts[1])
	}
	return "", filepath.Base(trimmed)
}

// destFromRequest resolves the upload destination from the request,
// supporting both path form (/api/upload/<dest> or /api/upload/<dest>/<fn>)
// and query form (?dest=<dest>).
func destFromRequest(c *gin.Context) string {
	if q := c.Query("dest"); q != "" {
		return q
	}
	dest, _ := parseUploadPath(c.Param("fn"))
	return dest
}

// filenameFromRequest extracts the original filename from the URL path, query,
// X-File-Name header, or Content-Disposition header without reading the body.
func filenameFromRequest(c *gin.Context) string {
	if _, fn := parseUploadPath(c.Param("fn")); fn != "" && fn != "." && fn != "/" {
		if decoded, err := url.QueryUnescape(fn); err == nil && decoded != "" {
			return filepath.Base(decoded)
		}
		return fn
	}
	if q := c.Query("filename"); q != "" {
		return filepath.Base(q)
	}
	if hdr := c.GetHeader("X-File-Name"); hdr != "" {
		if decoded, err := url.QueryUnescape(hdr); err == nil && decoded != "" {
			return filepath.Base(decoded)
		}
		return filepath.Base(hdr)
	}
	if cd := c.GetHeader("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if fn := params["filename"]; fn != "" {
				return filepath.Base(fn)
			}
		}
	}
	return ""
}

func ensureDriver(c *gin.Context, dest string) bool {
	if dest == "local" {
		if localDriver == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "local storage not configured"})
			return false
		}
		return true
	}
	if dest == "p.sda1.dev" {
		if pSda1DevDriver == nil && psda1ProxyDriver == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "p.sda1.dev storage not configured"})
			return false
		}
		return true
	}
	if r2Driver == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no storage backend available"})
		return false
	}
	return true
}

func uploadDriver(dest string) storage.StorageDriver {
	if dest == "local" {
		return localDriver
	}
	if dest == "p.sda1.dev" {
		if pSda1DevDriver != nil {
			return pSda1DevDriver
		}
		return psda1ProxyDriver
	}
	return r2Driver
}

// Upload streams the raw request body directly to the target backend without
// multipart parsing or in-memory buffering.
func Upload(c *gin.Context) {
	ContentType := c.GetHeader("Content-Type")
	hintFn := filenameFromRequest(c)
	dest := effectiveDest(destFromRequest(c), hintFn, ContentType)
	if !ensureDriver(c, dest) {
		return
	}

	ts := strconv.Itoa(int(time.Now().Unix()))
	fn := hintFn
	if fn == "" || fn == "/" || fn == "." {
		fn = uuid.New().String()[:8] + contentTypeToExtend(ContentType)
	}
	path := ts + "/" + fn

	drv := uploadDriver(dest)
	result, err := drv.Put(path, ContentType, c.Request.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if result != nil && result.Path != "" {
		path = result.Path
	}

	uploadResp(c, path, dest, result)
}

func CreateUploadSession(c *gin.Context) {
	Upload(c)
}

var Deleted = tools.NewLRUCache[string, int64](256)

// 假设你想设置过期时间为 30分钟
// 30分钟 = 1800秒 = 1,800,000 毫秒
// 在你的算法中，需要左移 16 位
const CacheExpireDelta = 1800 * 1000 << 16

func notSupported(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": "OneDrive API disabled"})
}

// OneDrive item IDs in this deployment all start with "01LL".
const onedriveIDPrefix = "01LL"

func validOneDriveID(id string) bool {
	return strings.HasPrefix(id, onedriveIDPrefix)
}

func Redirect(c *gin.Context) {
	id := c.Param("id")
	if !validOneDriveID(id) {
		c.Status(http.StatusNotFound)
		return
	}
	fn := c.Param("fn")
	prefix := os.Getenv("ENDPOINT_PREFIX") //ENDPOINT_PREFIX=https://uploads.moonchan.xyz/api/
	c.Redirect(301, prefix+id+"/"+fn)
}

func Get(c *gin.Context) {
	if azureDisabled {
		notSupported(c)
		return
	}
	id := c.Param("id")
	fn := c.Param("fn")
	if !validOneDriveID(id) {
		c.Status(http.StatusNotFound)
		return
	}
	ext := filepath.Ext(fn)

	// 1. 检查缓存中是否标记为已删除
	if timestamp, ok := Deleted.Get(id); ok {
		if tools.NewTimeStamp() < timestamp+CacheExpireDelta {
			c.Redirect(http.StatusFound, os.Getenv("default"))
			return
		}
	}

	// 2. 请求上游
	file, contentLength, contentType, err := agent.Get(id, fn)

	// 3. 错误处理
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	// !!! 关键修复：确保关闭 file !!!
	defer file.Close()

	// 4. 检查上游是否返回了特定的错误 JSON (Soft 404)
	if contentType == "application/json; odata.metadata=minimal; odata.streaming=true; IEEE754Compatible=false; charset=utf-8" {
		c.Status(http.StatusInternalServerError)
		return
	}

	// 5. 正常返回文件
	finalContentType := tools.Or(
		tools.Ternary(ext == ".webp", "image/webp", ""),
		mime.TypeByExtension(ext),
		contentType,
	)

	c.DataFromReader(http.StatusOK, contentLength, finalContentType, file, map[string]string{
		"Content-Disposition": "inline",
	})
}

func Delete(c *gin.Context) {
	if azureDisabled {
		notSupported(c)
		return
	}
	id := c.Param("id")
	if !validOneDriveID(id) {
		c.Status(http.StatusNotFound)
		return
	}

	_, err := agent.Delete(id)
	if err != nil {
		if errors.Is(err, io.EOF) {
			c.JSON(http.StatusOK, "ok")
		} else {
			c.JSON(http.StatusInternalServerError, err)
		}
	}

	Deleted.Put(id, tools.NewTimeStamp())
}

func DeleteWithKey(c *gin.Context) {
	if azureDisabled {
		notSupported(c)
		return
	}
	id := c.Param("id")
	key := c.Param("key")
	if !validOneDriveID(id) {
		c.Status(http.StatusNotFound)
		return
	}

	if hash(id) != key {
		c.JSON(http.StatusUnauthorized, "key is wrong")
		return
	}

	if _, ok := Deleted.Get(id); ok {
		c.JSON(http.StatusOK, "not found")
		return
	}

	_, err := agent.Delete(id)
	if err != nil {
		if errors.Is(err, io.EOF) {
			c.JSON(http.StatusOK, "ok")
		} else {
			c.JSON(http.StatusInternalServerError, err)
		}
		return
	}

	c.JSON(http.StatusGone, "gone")

	Deleted.Put(id, tools.NewTimeStamp())
}

func R2File(c *gin.Context) {
	raw := strings.TrimPrefix(c.Param("path"), "/")
	if raw == "" {
		c.Status(http.StatusNotFound)
		return
	}
	// strip the r2/ routing prefix - actual R2 key is just {ts}/{fn}
	fullPath := strings.TrimPrefix(raw, "r2/")

	if key := c.Query("delete"); key != "" {
		if key != hash(fullPath) {
			c.JSON(http.StatusUnauthorized, "key is wrong")
			return
		}
		if _, ok := Deleted.Get(fullPath); ok {
			c.JSON(http.StatusOK, "not found")
			return
		}
		err := r2Driver.Delete(fullPath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, err)
			return
		}
		Deleted.Put(fullPath, tools.NewTimeStamp())
		c.JSON(http.StatusGone, "gone")
		return
	}

	file, size, contentType, err := r2Driver.Get(fullPath)
	if err != nil {
		c.String(http.StatusNotFound, err.Error())
		return
	}
	defer file.Close()

	c.DataFromReader(http.StatusOK, size, contentType, file, map[string]string{
		"Content-Disposition": "inline",
	})
}

func LocalFile(c *gin.Context) {
	if localDriver == nil {
		c.Status(http.StatusNotFound)
		return
	}

	raw := strings.TrimPrefix(c.Param("path"), "/")
	if raw == "" {
		c.Status(http.StatusNotFound)
		return
	}

	if key := c.Query("delete"); key != "" {
		if key != hash(raw) {
			c.JSON(http.StatusUnauthorized, "key is wrong")
			return
		}
		if _, ok := Deleted.Get(raw); ok {
			c.JSON(http.StatusOK, "not found")
			return
		}
		err := localDriver.Delete(raw)
		if err != nil {
			c.JSON(http.StatusInternalServerError, err)
			return
		}
		Deleted.Put(raw, tools.NewTimeStamp())
		c.JSON(http.StatusGone, "gone")
		return
	}

	file, size, contentType, err := localDriver.Get(raw)
	if err != nil {
		c.String(http.StatusNotFound, err.Error())
		return
	}
	defer file.Close()

	c.DataFromReader(http.StatusOK, size, contentType, file, map[string]string{
		"Content-Disposition": "inline",
	})
}

func P_sda1_devFile(c *gin.Context) {
	if pSda1DevDriver == nil {
		c.Status(http.StatusNotFound)
		return
	}

	raw := strings.TrimPrefix(c.Param("path"), "/")
	if raw == "" {
		c.Status(http.StatusNotFound)
		return
	}

	file, size, contentType, err := pSda1DevDriver.Get(raw)
	if err != nil {
		c.String(http.StatusNotFound, err.Error())
		return
	}
	defer file.Close()

	c.DataFromReader(http.StatusOK, size, contentType, file, map[string]string{
		"Content-Disposition": "inline",
	})
}

func hash(s string) string {
	hash := sha256.Sum256([]byte(s + agent.SALT))
	hashString := fmt.Sprintf("%x", hash[:8])
	return hashString
}
