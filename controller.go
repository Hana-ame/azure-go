package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

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

var Deleted = tools.NewLRUCache[string, bool](256)

func Get(c *gin.Context) {
	id := c.Param("id")
	fn := c.Param("fn")

	if _, ok := Deleted.Get(id); ok {
		// c.JSON(http.StatusGone, "gone")
		c.Redirect(http.StatusFound, os.Getenv("default")) // 替换成你想要重定向的 URL
		return
	}

	file, contentLength, contentType, err := agent.Get(id, fn)
	if err != nil {
		Deleted.Put(id, true)
		// c.JSON(http.StatusInternalServerError, err)
		c.Redirect(http.StatusFound, os.Getenv("default")) // 替换成你想要重定向的 URL
		return
	}

	if contentType == "application/json; odata.metadata=minimal; odata.streaming=true; IEEE754Compatible=false; charset=utf-8" {
		Deleted.Put(id, true)
		c.Redirect(http.StatusFound, os.Getenv("default")) // 替换成你想要重定向的 URL
		return
	}

	c.DataFromReader(http.StatusOK, contentLength, contentType, file, map[string]string{"Content-Disposition": "inline"})
}

func Delete(c *gin.Context) {
	id := c.Param("id")
	if c.Query("delete") != "delete" {
		c.AbortWithStatus(http.StatusNotFound)
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
}

func DeleteWithKey(c *gin.Context) {
	id := c.Param("id")
	key := c.Param("key")

	if hash(id) != key {
		c.JSON(http.StatusUnauthorized, "key is wrong")
		return
	}

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

	Deleted.Put(id, true)

	c.JSON(http.StatusGone, "gone")
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
