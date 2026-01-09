package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	tools "github.com/Hana-ame/azure-go/Tools"
	"github.com/Hana-ame/azure-go/Tools/orderedmap"
	"github.com/gin-gonic/gin"
)

// note: it works when local
func Upload(c *gin.Context) {
	ContentType := c.GetHeader("Content-Type")
	ContentLength := c.GetHeader("Content-Length")
	body := c.Request.Body // just use Body to get it https://stackoverflow.com/questions/46579429/golang-cant-get-body-from-request-getbody

	resp, err := agent.Upload(ContentType, ContentLength, body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err)
		return
	}

	id := resp.GetOrDefault("id", "failed").(string)
	o := orderedmap.New()
	o.Set("id", id)
	o.Set("key", hash(id))

	c.JSON(http.StatusOK, o)
	// c.JSON(http.StatusOK, resp)
}

var Deleted = tools.NewLRUCache[string, int64](256)

// 假设你想设置过期时间为 30分钟
// 30分钟 = 1800秒 = 1,800,000 毫秒
// 在你的算法中，需要左移 16 位
const CacheExpireDelta = 1800 * 1000 << 16

func Get(c *gin.Context) {
	id := c.Param("id")
	fn := c.Param("fn")
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
	id := c.Param("id")

	// 小限制,暂时取消
	// if c.Query("delete") != "delete" {
	// 	c.AbortWithStatus(http.StatusNotFound)
	// 	return
	// }

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
	id := c.Param("id")
	key := c.Param("key")

	if hash(id) != key {
		c.JSON(http.StatusUnauthorized, "key is wrong")
		return
	}

	// 在cache中找到,说明已经删除
	if _, ok := Deleted.Get(id); ok {
		c.JSON(http.StatusOK, "not found")
		return
	}

	// TODO: what if deleted an unexist one?
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

func CreateUploadSession(c *gin.Context) {
	ContentType := c.GetHeader("Content-Type")

	resp, err := agent.CreateUploadSession(ContentType, nil)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func hash(s string) string {
	hash := sha256.Sum256([]byte(s + agent.SALT))
	hashString := fmt.Sprintf("%x", hash[:8])
	return hashString
}
