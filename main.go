package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/Hana-ame/azure-go/Tools/debug"
	middleware "github.com/Hana-ame/azure-go/Tools/my_gin_middleware"
	"github.com/Hana-ame/azure-go/Tools/myiter"
	"github.com/Hana-ame/azure-go/Tools/orderedmap"
	"github.com/Hana-ame/azure-go/myfetch"
	localfs "github.com/Hana-ame/azure-go/storage/local"
	p_sda1_dev "github.com/Hana-ame/azure-go/storage/p_sda1_dev"
	"github.com/Hana-ame/azure-go/storage/r2"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/joho/godotenv/autoload"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// global
var agent *Agent
var r2Driver *r2.Driver
var r2PublicURL string

const Version = "v26.08.08"

func main_with_redirect() {

	r := gin.Default()
	r.Use(middleware.CORSMiddleware())
	r.PUT("/api/upload", Upload)
	r.POST("/api/upload", CreateUploadSession)
	r.GET("/api/:id/*fn", Redirect)
	r.DELETE("/api/:id/:key", Delete) // 不能这么做的样子
	r.GET("/api/delete/:id/:key", DeleteWithKey)

	r.Run(":8080")
}

func main() {

	myfetch.DefaultClient = &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   90 * time.Second,
				KeepAlive: 90 * time.Second,
			}).DialContext,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			MaxConnsPerHost:     200,
			IdleConnTimeout:     10 * time.Second,
			TLSHandshakeTimeout: 30 * time.Second,
		},
		Timeout: 300 * time.Second,
	}

	log.Printf("[Init] Version: %s", Version)

	cwd, _ := os.Getwd()
	log.Printf("[Init] Current Working Directory: %s", cwd)

	if err := godotenv.Load("access_token"); err != nil {
		log.Printf("[Init] Failed to load access_token file: %v", err)
	} else {
		log.Println("[Init] Successfully loaded from file: access_token")
	}

	secretBytes, err := os.ReadFile("client_secrets.txt")
	if err != nil {
		log.Printf("[Init] Failed to read client_secrets.txt: %v", err)
	} else {
		log.Println("[Init] Successfully loaded client_secrets.txt")
	}

	overloadEnvByJsonFile(".json")

	var clientSecret string
	if len(secretBytes) > 0 {
		clientSecret = strings.TrimSpace(string(secretBytes))
	}

	agent = &Agent{
		tenent_id:     os.Getenv("tenent_id"),
		client_id:     os.Getenv("client_id"),
		redirect_url:  os.Getenv("redirect_url"),
		client_secret: clientSecret,
		scope:         os.Getenv("scope"),

		access_token:  os.Getenv("access_token"),
		refresh_token: "", // no longer used
		SALT:          os.Getenv("SALT"),
		user_id:       os.Getenv("user_id"),
	}

	log.Printf("[Token Loaded] access_token length: %d", len(agent.access_token))
	log.Printf("[Init] client_secret set: %v, user_id=%s", agent.client_secret != "", agent.user_id)

	log.Printf("[Init] Agent Config: tenent_id=%s, client_id=%s, scope=%s", agent.tenent_id, agent.client_id, agent.scope)
	// printStructFields(*agent) // bug

	r2AccountID := os.Getenv("R2_ACCOUNT_ID")
	r2AccessKey := os.Getenv("R2_ACCESS_KEY_ID")
	r2SecretKey := os.Getenv("R2_SECRET_ACCESS_KEY")
	r2Bucket := os.Getenv("R2_BUCKET_NAME")
	r2PublicURL = os.Getenv("R2_PUBLIC_URL")

	if r2AccountID != "" && r2AccessKey != "" && r2SecretKey != "" && r2Bucket != "" {
		r2Endpoint := r2AccountID + ".r2.cloudflarestorage.com"
		r2Client, err := minio.New(r2Endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(r2AccessKey, r2SecretKey, ""),
			Secure: true,
		})
		if err != nil {
			log.Printf("[Init] Failed to create R2 client: %v", err)
		} else {
			r2Driver = r2.New(r2Client, r2Bucket, "")
			log.Printf("[Init] R2 driver initialized: bucket=%s, endpoint=%s", r2Bucket, r2Endpoint)
		}
	} else {
		log.Printf("[Init] R2 env vars incomplete, R2 driver disabled")
	}

	if os.Getenv("AZURE_DISABLE") == "true" {
		azureDisabled = true
		log.Printf("[Init] Azure API disabled by AZURE_DISABLE=true")
	}

	localStoragePath := os.Getenv("LOCAL_STORAGE_PATH")
	if localStoragePath == "" {
		localStoragePath = "."
	}
	localDrv, err := localfs.New(localStoragePath)
	if err != nil {
		log.Printf("[Init] Failed to create local driver: %v", err)
	} else {
		localDriver = localDrv
		log.Printf("[Init] Local driver initialized: root=%s", localStoragePath)
	}

	pSda1DevDriver = p_sda1_dev.New()
	log.Printf("[Init] p.sda1.dev driver initialized")

	// Deamon
	if !azureDisabled {
		go func() {
			for {
				for err := agent.RenewToken(); err != nil; err = agent.RenewToken() {
					debug.W("renew", err)
					time.Sleep(1 * time.Second)
				}
				time.Sleep(20 * time.Minute)
			}
		}()
	} else {
		log.Printf("[Init] Token renewal skipped (Azure disabled)")
	}

	// go main_with_redirect() // not used.

	r := gin.Default()
	r.Use(middleware.CORSMiddleware())

	// Original API
	r.PUT("/api/upload", Upload)
	r.PUT("/api/upload/*fn", Upload)
	r.POST("/api/upload", CreateUploadSession)
	r.GET("/api/:id/*fn", Get)
	r.DELETE("/api/:id/:key", Delete) // 不能这么做的样子
	r.GET("/api/delete/:id/:key", DeleteWithKey)

	// R2 direct access: GET /r2/path?delete=key
	r.GET("/r2/*path", R2File)

	// Local filesystem access: GET /local/path?delete=key
	r.GET("/local/*path", LocalFile)

	// p.sda1.dev proxy: GET /p.sda1.dev/path
	r.GET("/p.sda1.dev/*path", P_sda1_devFile)

	// Static files (embedded)
	r.GET("/", func(c *gin.Context) {
		data, _ := embeddedStatic.ReadFile("static/index.html")
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})
	r.GET("/index.html", func(c *gin.Context) {
		data, _ := embeddedStatic.ReadFile("static/index.html")
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})
	r.GET("/sw.js", func(c *gin.Context) {
		data, _ := embeddedStatic.ReadFile("static/sw.js")
		c.Data(http.StatusOK, "application/javascript", data)
	})
	r.GET("/manifest.json", func(c *gin.Context) {
		data, _ := embeddedStatic.ReadFile("static/manifest.json")
		c.Data(http.StatusOK, "application/json", data)
	})
	r.GET("/favicon.ico", func(c *gin.Context) {
		data, _ := embeddedStatic.ReadFile("static/favicon.ico")
		c.Data(http.StatusOK, "image/x-icon", data)
	})

	// S3-compatible API
	hashStore, err := NewSQLiteHashStore("hashes.db")
	if err != nil {
		log.Printf("WARN: hash store init failed, dedup disabled: %v", err)
	}

	pathStore, err := NewSQLitePathStore("paths.db")
	if err != nil {
		log.Printf("WARN: path store init failed: %v", err)
	}

	azureDriver = NewAzureDriver(agent, hashStore, pathStore)
	s3 := NewS3Controller(azureDriver)
	if localDriver != nil {
		s3.SetLocalDriver(localDriver)
	}
	r.PUT("/:bucket/*key", s3.PutObject)
	r.GET("/:bucket/*key", s3.GetObject)
	r.DELETE("/:bucket/*key", s3.DeleteObject)
	r.HEAD("/:bucket/*key", s3.HeadObject)

	// Dedicated bucket server on 127.26.7.10:8080
	go func() {
		if err := ListenBucket("127.26.7.10:8080", "mybucket", azureDriver, os.Getenv("S3_AUTH_TOKEN")); err != nil {
			log.Printf("[BucketServer] failed: %v", err)
		}
	}()

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	r.Run(addr)
}

// this function receive json request.
func readJsonFromFile(fn string) (*orderedmap.OrderedMap, error) {
	jsonFile, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	o := orderedmap.New()
	err = json.NewDecoder(jsonFile).Decode(&o)
	return o, err
}

func overloadToEnv(o *orderedmap.OrderedMap) {
	iter := myiter.NewIter(o)
	iter.Iter(func(k, v any) bool {
		key, keyIsString := k.(string)
		value, valueIsString := v.(string)
		if keyIsString && valueIsString {
			_ = os.Setenv(key, value)
		}
		return false
	})
}

func overloadEnvByJsonFile(fn string) error {
	if o, err := readJsonFromFile(fn); err == nil {
		log.Printf("[Init] Overloading environment from JSON file: %s", fn)
		overloadToEnv(o)
	} else {
		return err
	}
	return nil
}

// by GPT
// can't pass pointer..
func printStructFields(s interface{}) {
	val := reflect.ValueOf(s)

	// Make sure the input is a struct
	if val.Kind() != reflect.Struct {
		fmt.Println("Input is not a struct")
		return
	}

	// Iterate through the fields
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldName := val.Type().Field(i).Name
		fieldValue := field.Interface()

		log.Printf("%s: %v\n", fieldName, fieldValue)
	}
}
