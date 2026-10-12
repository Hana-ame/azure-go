package main

import (
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
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

	if dest == "azure" && result != nil {
		// OneDrive item ID from upstream; delete key is hash of the ID
		id := result.ID
		reqBase := publicBaseURL(c)
		o.Set("id", id)
		o.Set("key", hash(id))
		o.Set("path", "/api/"+id)
		o.Set("url", reqBase+"/api/"+id+"/")
		o.Set("delete_url", reqBase+"/api/delete/"+id+"/"+hash(id))
		c.JSON(http.StatusOK, o)
		return
	}

	if dest == "p.sda1.dev" {
		o.Set("path", "/p.sda1.dev/"+path)
		// Upload URL: proxied through /api/p.sda1.dev (with error caching)
		reqBase := publicBaseURL(c)
		o.Set("url", reqBase+"/api/p.sda1.dev/"+path)
		// Delete URL: direct to p.sda1.dev (NOT proxied)
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

// effectiveDest resolves the actual upload backend for a request.
// An explicit ?dest= always wins; with no dest, the default is azure
// (falling back to r2 only when azure is unavailable, e.g. local dev).
func effectiveDest(dest string) string {
	if dest == "" {
		if azureDisabled || azureDriver == nil {
			return "r2"
		}
		return "azure"
	}
	return dest
}

// destFromRequest resolves the upload destination from the request,
// supporting both path form (/api/upload/<dest>, no query params) and
// the legacy query form (?dest=<dest>). Path takes precedence so the
// frontend can drop query params entirely.
func destFromRequest(c *gin.Context) string {
	if q := c.Query("dest"); q != "" {
		return q
	}
	if fn := c.Param("fn"); fn != "" {
		return strings.TrimPrefix(fn, "/")
	}
	return ""
}

func ensureDriver(c *gin.Context) bool {
	dest := effectiveDest(destFromRequest(c))
	if dest == "local" {
		if localDriver == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "local storage not configured"})
			return false
		}
		return true
	}
	if dest == "p.sda1.dev" {
		if psda1ProxyDriver == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "p.sda1.dev storage not configured"})
			return false
		}
		return true
	}
	if dest == "azure" {
		if azureDisabled || azureDriver == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "azure storage not configured"})
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

func uploadDriver(c *gin.Context) storage.StorageDriver {
	dest := effectiveDest(destFromRequest(c))
	if dest == "local" {
		return localDriver
	}
	if dest == "p.sda1.dev" {
		return psda1ProxyDriver
	}
	if dest == "azure" {
		return azureDriver
	}
	return r2Driver
}

// note: it works when local
func Upload(c *gin.Context) {
	if !ensureDriver(c) {
		return
	}

	ContentType := c.GetHeader("Content-Type")
	body := c.Request.Body

	dest := effectiveDest(destFromRequest(c))

	// Azure legacy mode: use Agent.Upload directly, exactly like the original img/ layout.
	if dest == "azure" {
		resp, err := agent.Upload(ContentType, c.GetHeader("Content-Length"), body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, err)
			return
		}
		id, _ := resp.GetOrDefault("id", "").(string)
		uploadResp(c, "", "azure", &storage.PutResult{ID: id})
		return
	}

	ts := strconv.Itoa(int(time.Now().Unix()))
	fn := strings.TrimPrefix(c.Param("fn"), "/")
	if fn == "" || fn == "/" {
		fn = uuid.New().String()[:8] + contentTypeToExtend(ContentType)
	}
	path := ts + "/" + fn

	drv := uploadDriver(c)
	result, err := drv.Put(path, ContentType, body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err)
		return
	}
	if result != nil && result.Path != "" {
		path = result.Path
	}

	uploadResp(c, path, dest, result)
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
		// 如果 当前时间 > (记录时间 + 过期时长)，说明缓存已过期，不再拦截
		// 或者你的逻辑是：只要在过期时间内，就拦截？
		// 你的原代码：timestamp + delta < now
		// 意思是：如果 (记录时间 + 1.8秒) 小于 当前时间 ——> 即记录时间是很久以前的
		// 也就说：只有 1.8 秒内的记录会被认为是“未过期”并拦截？这看起来像是在防抖动。

		// 修正后的逻辑：如果是想封禁30分钟
		if tools.NewTimeStamp() < timestamp+CacheExpireDelta {
			c.Redirect(http.StatusFound, os.Getenv("default"))
			return
		}
		// 如果超过时间了，可能想重试？或者应该从 Cache 中移除？
	}

	// 2. 请求上游
	file, contentLength, contentType, err := agent.Get(id, fn)

	// 3. 错误处理
	if err != nil {
		// 记录日志，不要直接把 err 给前端
		// fmt.Printf("Error getting file %s: %v\n", id, err)

		// if strings.Contains(err.Error(), "connection reset by peer") {
		// 	// 避免死循环，最好限制重试次数，或者直接返回 502
		// 	c.String(http.StatusBadGateway, "Upstream connection reset, please try again.")
		// 	return
		// }

		// c.String(http.StatusInternalServerError, "Internal Service Error")
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	// !!! 关键修复：确保关闭 file !!!
	defer file.Close()

	// 4. 检查上游是否返回了特定的错误 JSON (Soft 404)
	if contentType == "application/json; odata.metadata=minimal; odata.streaming=true; IEEE754Compatible=false; charset=utf-8" {
		// 标记为已删除
		// Deleted.Put(id, tools.NewTimeStamp())

		// 文件不存在/已删除，直接重定向到默认图，忽略 body
		// c.Redirect(http.StatusFound, os.Getenv("default"))
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

func CreateUploadSession(c *gin.Context) {
	if !ensureDriver(c) {
		return
	}

	ct := c.GetHeader("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		file, header, err := c.Request.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no file in form"})
			return
		}
		defer file.Close()

		dest := effectiveDest(destFromRequest(c))

		// Azure legacy mode: use Agent.Upload directly, exactly like the original img/ layout.
		if dest == "azure" {
			resp, err := agent.Upload(header.Header.Get("Content-Type"), strconv.FormatInt(header.Size, 10), file)
			if err != nil {
				c.JSON(http.StatusInternalServerError, err)
				return
			}
			id, _ := resp.GetOrDefault("id", "").(string)
			uploadResp(c, "", "azure", &storage.PutResult{ID: id})
			return
		}

		fn := header.Filename
		if fn == "" {
			fn = uuid.New().String()[:8] + contentTypeToExtend(header.Header.Get("Content-Type"))
		}

		ts := strconv.Itoa(int(time.Now().Unix()))
		path := ts + "/" + fn

		drv := uploadDriver(c)
		result, err := drv.Put(path, header.Header.Get("Content-Type"), file)
		if err != nil {
			c.JSON(http.StatusInternalServerError, err)
			return
		}
		if result != nil && result.Path != "" {
			path = result.Path
		}

		uploadResp(c, path, dest, result)
		return
	}
	Upload(c)
}

func hash(s string) string {
	hash := sha256.Sum256([]byte(s + agent.SALT))
	hashString := fmt.Sprintf("%x", hash[:8])
	return hashString
}
