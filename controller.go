package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func Upload(c *gin.Context) {
	ContentType := c.GetHeader("Content-Type")
	body, err := c.Request.GetBody()
	if err != nil {
		c.JSON(http.StatusBadRequest, err)
	}

	resp, err := agent.Upload(ContentType, body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err)
	}

	c.JSON(http.StatusOK, resp)
}

func Get(c *gin.Context) {
	id := c.Param("id")

	file, contentLength, contentType, err := agent.Get(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err)
	}

	c.DataFromReader(http.StatusOK, contentLength, contentType, file, nil)
}

func Delete(c *gin.Context) {
	id := c.Param("id")

	resp, err := agent.Delete(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err)
	}

	c.JSON(http.StatusOK, resp)
}
