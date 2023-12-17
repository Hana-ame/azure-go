package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/joho/godotenv/autoload"
)

func TestTest(t *testing.T) {
	now := time.Now().Unix()
	fmt.Println(now)
	env, _ := godotenv.Unmarshal("KEY=value")
	godotenv.Write(env, "./.env")
	fmt.Println(accessToken())

}

func TestRenew(t *testing.T) {
	godotenv.Load("refresh_token")
	agent := Agent{
		tenent_id:     os.Getenv("tenent_id"),
		client_id:     os.Getenv("client_id"),
		redirect_url:  os.Getenv("redirect_url"),
		client_secret: os.Getenv("client_secret"),
		scope:         os.Getenv("scope"),

		// access_token
		// expires_time
		refresh_token: os.Getenv("refresh_token"),
	}

	fmt.Println(agent) // pass

	agent.RenewToken()
	fmt.Println(agent) // pass

}

func TestUpload(t *testing.T) {
	godotenv.Load("access_token")
	agent := Agent{
		tenent_id:     os.Getenv("tenent_id"),
		client_id:     os.Getenv("client_id"),
		redirect_url:  os.Getenv("redirect_url"),
		client_secret: os.Getenv("client_secret"),
		scope:         os.Getenv("scope"),

		access_token: accessToken(),
		// expires_time
		refresh_token: os.Getenv("refresh_token"),
	}
	// str := readFileToString("1.jpg")
	f, _ := os.Open("1.jpg")
	r, err := agent.Upload("image/jpeg", f)
	fmt.Println(err)
	fmt.Println(r)
}

func TestGet(t *testing.T) {
	id := "01LLWEUU3FBIELXXTESJBJLXTICCTSLP7S"
	id = "01LLWEUUYYXMU2N4A4A5G3MYUDWS6IQSDW"

	godotenv.Load("access_token")
	agent := Agent{
		tenent_id:     os.Getenv("tenent_id"),
		client_id:     os.Getenv("client_id"),
		redirect_url:  os.Getenv("redirect_url"),
		client_secret: os.Getenv("client_secret"),
		scope:         os.Getenv("scope"),

		access_token: accessToken(),
		// expires_time
		refresh_token: os.Getenv("refresh_token"),
	}
	// str := readFileToString("1.jpg")
	r, _, _, _ := agent.Get(id)
	readerSaveToFile(r)
}

func TestDelete(t *testing.T) {
	id := "01LLWEUU3FBIELXXTESJBJLXTICCTSLP7S"

	godotenv.Load("access_token")
	agent := Agent{
		tenent_id:     os.Getenv("tenent_id"),
		client_id:     os.Getenv("client_id"),
		redirect_url:  os.Getenv("redirect_url"),
		client_secret: os.Getenv("client_secret"),
		scope:         os.Getenv("scope"),

		access_token: accessToken(),
		// expires_time
		refresh_token: os.Getenv("refresh_token"),
	}
	// str := readFileToString("1.jpg")
	r, _ := agent.Delete(id)
	fmt.Println(r)
}

func TestMine(t *testing.T) {
	s := contentTypeToExtend("image/png")
	log.Println(s)
}

func readFileToString(fn string) string {
	file, err := os.ReadFile(fn)
	if err != nil {
		log.Println(err)
	}
	return string(file)
}

func accessToken() string {
	jsonFIle, _ := os.ReadFile(".json")
	var m map[string]any

	json.Unmarshal(jsonFIle, &m)

	return m[`access_token`].(string)
}

func readerSaveToFile(reader io.Reader) {
	f, _ := os.Create("out.jpg") // 可以create同一个文件名的
	w := bufio.NewWriter(f)
	io.Copy(w, reader)
}
