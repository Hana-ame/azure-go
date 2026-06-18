package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"reflect"
	"time"

	"github.com/Hana-ame/azure-go/Tools/debug"
	middleware "github.com/Hana-ame/azure-go/Tools/my_gin_middleware"
	"github.com/Hana-ame/azure-go/Tools/myiter"
	"github.com/Hana-ame/azure-go/Tools/orderedmap"
	"github.com/Hana-ame/azure-go/myfetch"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/joho/godotenv/autoload"
)

// global
var agent *Agent

func main_with_redirect() {

	r := gin.Default()
	r.Use(middleware.CORSMiddleware())
	r.PUT("/api/upload", Upload)
	r.POST("/api/upload", CreateUploadSession)
	r.GET("/api/:id/*fn", Redirect)
	r.DELETE("/api/:id/:key", Delete) // 不能这么做的样子
	r.GET("/api/delete/:id/:key", DeleteWithKey)

	r.Run("127.25.11.27:8080")
}

func main() {

	myfetch.DefaultClient = &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   90 * time.Second,
				KeepAlive: 90 * time.Second,
			}).DialContext,
			MaxIdleConns:        100,
			IdleConnTimeout:     10 * time.Second,
			TLSHandshakeTimeout: 30 * time.Second,
		},
		Timeout: 300 * time.Second,
	}

	godotenv.Load("refresh_token")
	overloadEnvByJsonFile(".json")

	agent = &Agent{
		tenent_id:     os.Getenv("tenent_id"),
		client_id:     os.Getenv("client_id"),
		redirect_url:  os.Getenv("redirect_url"),
		client_secret: os.Getenv("client_secret"),
		scope:         os.Getenv("scope"),

		access_token: os.Getenv("access_token"),
		// expires_time
		refresh_token: os.Getenv("refresh_token"),
		SALT:          os.Getenv("SALT"),
	}

	log.Println(agent)
	// printStructFields(*agent) // bug

	// Deamon
	go func() {
		for {
			for err := agent.RenewToken(); err != nil; err = agent.RenewToken() {
				debug.W("renew", err)
			}
			// if Deleted.Len() > 64 {
			// 	Deleted = &syncmapwithcnt.SyncMapWithCount{}
			// }
			time.Sleep(20 * time.Minute)
		}
	}()

	// go main_with_redirect() // not used.

	r := gin.Default()
	r.Use(middleware.CORSMiddleware())
	r.PUT("/api/upload", Upload)
	r.POST("/api/upload", CreateUploadSession)
	r.GET("/api/:id/*fn", Get)
	r.DELETE("/api/:id/:key", Delete) // 不能这么做的样子
	r.GET("/api/delete/:id/:key", DeleteWithKey)

	r.Run("127.23.12.17:8080")
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
		overloadToEnv(o)
	} else {
		log.Println(err)
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
