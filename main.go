package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Hana-ame/orderedmap"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/joho/godotenv/autoload"
)

var agent *Agent

func main() {
	godotenv.Load("refresh_token")
	o, _ := JsonFromFile(".json")
	agent = &Agent{
		tenent_id:     os.Getenv("tenent_id"),
		client_id:     os.Getenv("client_id"),
		redirect_url:  os.Getenv("redirect_url"),
		client_secret: os.Getenv("client_secret"),
		scope:         os.Getenv("scope"),

		access_token: o.GetOrDefault("access_token", "").(string),
		// expires_time
		refresh_token: o.GetOrDefault("refresh_token", "").(string),
		SALT:          os.Getenv("refresh_token"),
	}

	go func() {
		for {
			time.Sleep(20 * time.Minute)
			agent.RenewToken()
		}
	}()

	r := gin.Default()
	r.PUT("/api/upload", Upload)
	r.GET("/api/:id/*fn", Get)
	r.DELETE("/api/:id/:key", Delete)

	r.Run("127.23.12.17:8080")
}

// this function receive json request.
func JsonFromFile(fn string) (*orderedmap.OrderedMap, error) {
	jsonFile, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	o := orderedmap.New()
	err = json.NewDecoder(jsonFile).Decode(&o)
	return o, err
}

func hash(s string) string {
	hash := sha256.Sum256([]byte(s + agent.SALT))
	hashString := fmt.Sprintf("%x", hash[:2])
	return hashString
}
