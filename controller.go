package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Hana-ame/azure-go/syncmapwithcnt"
	"github.com/Hana-ame/orderedmap"
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

func Get(c *gin.Context) {
	id := c.Param("id")

	file, contentLength, contentType, err := agent.Get(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err)
	}

	c.DataFromReader(http.StatusOK, contentLength, contentType, file, map[string]string{"Content-Disposition": "inline"})
}

var Deleted = syncmapwithcnt.New()

func Delete(c *gin.Context) {
	id := c.Param("id")
	key := c.Param("key")

	if hash(id) != key {
		c.JSON(http.StatusUnauthorized, "key is wrong")
		return
	}

	if _, ok := Deleted.Load(id); ok {
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

	Deleted.Store(id, time.Now().Unix())
	c.JSON(http.StatusOK, "not found")
	return
}

func hash(s string) string {
	hash := sha256.Sum256([]byte(s + agent.SALT))
	hashString := fmt.Sprintf("%x", hash[:8])
	return hashString
}
