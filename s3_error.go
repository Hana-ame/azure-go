package main

import (
	"encoding/xml"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// S3ErrorResponse is the standard S3 XML error format.
type S3ErrorResponse struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource"`
	RequestID string   `xml:"RequestId"`
}

func writeS3Error(c *gin.Context, status int, code, message, resource string) {
	c.Header("Content-Type", "application/xml")
	c.Header("x-amz-request-id", uuid.New().String())
	c.XML(status, S3ErrorResponse{
		Code:      code,
		Message:   message,
		Resource:  resource,
		RequestID: uuid.New().String(),
	})
}
