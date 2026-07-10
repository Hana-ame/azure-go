package main

import (
	"net/http"
	"strings"

	"github.com/Hana-ame/azure-go/storage"
	"github.com/gin-gonic/gin"
)

// ListenBucket starts an HTTP server on addr that serves all S3 operations
// from a single fixed bucket. The URL path is used as the object key.
//
// If bearerToken is non-empty, all requests must include
// Authorization: Bearer <bearerToken>.
//
//   PUT /path/to/file      -> uploads to <bucket>/path/to/file
//   GET /path/to/file      -> retrieves <bucket>/path/to/file
//   HEAD /path/to/file     -> headers for <bucket>/path/to/file
//   DELETE /path/to/file   -> deletes <bucket>/path/to/file
func ListenBucket(addr, bucket string, driver storage.StorageDriver, bearerToken string) error {
	r := gin.New()
	r.Use(gin.Recovery())

	s3 := NewS3Controller(driver)

	// inject fixed bucket param so S3Controller reuses existing logic
	fixedBucket := func(c *gin.Context) {
		c.Params = append(c.Params, gin.Param{Key: "bucket", Value: bucket})
		c.Next()
	}

	// bearer auth
	var auth gin.HandlerFunc
	if bearerToken != "" {
		auth = func(c *gin.Context) {
			h := c.GetHeader("Authorization")
			if !strings.HasPrefix(h, "Bearer ") || strings.TrimSpace(h[7:]) != bearerToken {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}
			c.Next()
		}
	} else {
		auth = func(c *gin.Context) { c.Next() }
	}

	r.PUT("/*key", fixedBucket, auth, s3.PutObject)
	r.GET("/*key", fixedBucket, auth, s3.GetObject)
	r.DELETE("/*key", fixedBucket, auth, s3.DeleteObject)
	r.HEAD("/*key", fixedBucket, auth, s3.HeadObject)

	return r.Run(addr)
}
