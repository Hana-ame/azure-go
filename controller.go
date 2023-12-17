package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

func Upload(c *gin.Context) {
	ContentType := c.GetHeader("Content-Type")
	ContentLength := c.GetHeader("Content-Length")
	body := c.Request.Body // just use Body to get it https://stackoverflow.com/questions/46579429/golang-cant-get-body-from-request-getbody

	resp, err := agent.Upload(ContentType, ContentLength, body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err)
		return
	}

	// id := resp.GetOrDefault("id", "failed").(string)
	// o := orderedmap.New()
	// o.Set("id", id)
	// o.Set("key", hash(id))

	c.JSON(http.StatusOK, resp)
}

func Get(c *gin.Context) {
	id := c.Param("id")

	file, contentLength, contentType, err := agent.Get(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err)
	}

	c.DataFromReader(http.StatusOK, contentLength, contentType, file, map[string]string{"Content-Disposition": "inline"})
}

func Delete(c *gin.Context) {
	id := c.Param("id")
	// key := c.Param("key")

	// if hash(id) != key {
	// 	c.JSON(http.StatusUnauthorized, "key is wrong")
	// 	return
	// }

	_, err := agent.Delete(id)
	if err != nil {
		if errors.Is(err, io.EOF) {
			c.JSON(http.StatusOK, "ok")
		} else {
			c.JSON(http.StatusInternalServerError, err)
		}
		return
	}

	c.JSON(http.StatusOK, "not found")
}

func hash(s string) string {
	hash := sha256.Sum256([]byte(s + agent.SALT))
	hashString := fmt.Sprintf("%x", hash[:2])
	return hashString
}
