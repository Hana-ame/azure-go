package main

import (
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/joho/godotenv/autoload"
)

var agent *Agent

func main() {
	godotenv.Load("refresh_token")
	agent = &Agent{
		tenent_id:     os.Getenv("tenent_id"),
		client_id:     os.Getenv("client_id"),
		redirect_url:  os.Getenv("redirect_url"),
		client_secret: os.Getenv("client_secret"),
		scope:         os.Getenv("scope"),

		// access_token
		// expires_time
		refresh_token: os.Getenv("refresh_token"),
	}

	r := gin.Default()
	r.PUT("/api/upload", Upload)
	r.GET("/api/:id/*fn", Get)
	r.DELETE("/api/:id/*fn", Delete)

	r.Run("127.1.12.10:8080")
}
