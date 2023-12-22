package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"reflect"
	"time"

	"github.com/Hana-ame/azure-go/myiter"
	"github.com/Hana-ame/azure-go/syncmapwithcnt"
	"github.com/Hana-ame/orderedmap"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/joho/godotenv/autoload"
)

var agent *Agent

func main() {
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
			agent.RenewToken()
			if Deleted.Len() > 64 {
				Deleted = &syncmapwithcnt.SyncMapWithCount{}
			}
			time.Sleep(20 * time.Minute)
		}
	}()

	r := gin.Default()
	r.PUT("/api/upload", Upload)
	r.GET("/api/:id/*fn", Get)
	// r.DELETE("/api/:id/:key", Delete) // 不能这么做的样子
	r.GET("/api/delete/:id/:key", Delete)

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
