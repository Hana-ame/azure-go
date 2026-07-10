package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"testing"
	"time"

	tools "github.com/Hana-ame/azure-go/Tools"
	"github.com/Hana-ame/azure-go/Tools/debug"
	"github.com/Hana-ame/azure-go/myfetch"
	"github.com/joho/godotenv"
	_ "github.com/joho/godotenv/autoload"
)

func TestTest(t *testing.T) {
	skipIfNoCreds(t)
	now := time.Now().Unix()
	fmt.Println(now)
	env, _ := godotenv.Unmarshal("KEY=value")
	godotenv.Write(env, "./.env")
	fmt.Println(accessToken())

}

func TestXxx(t *testing.T) {
	skipIfNoCreds(t)
	godotenv.Load(".env")
	godotenv.Load("refresh_token")

	endpoint := `https://login.microsoftonline.com/` + os.Getenv("tenent_id") + `/oauth2/v2.0/token`

	body, _ := myfetch.URLEncodedFormReader(map[string]string{
		"client_id":     os.Getenv("client_id"),
		"refresh_token": os.Getenv("refresh_token"),
		"grant_type":    "refresh_token",
		// "client_secret": agt.client_secret,
	})
	resp, err := myfetch.Fetch(
		http.MethodPost,
		endpoint,
		map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
			"Origin":       os.Getenv("origin"),
		},
		body,
	)
	if err != nil {
		panic(err)
	}

	result, err := myfetch.ResponseToJson(resp)
	if err != nil {
		panic(err)
	}

	fmt.Println(result)

	fmt.Println("result:", result)

	o, _ := tools.ReadJSONFile(".json")
	o.Set("access_token", result.GetOrDefault("access_token", "agent.access_token").(string))
	tools.WriteJSONToFile(".json", o)
}

func TestRenew(t *testing.T) {
	skipIfNoCreds(t)
	godotenv.Load("refresh_token")
	agent := Agent{
		tenent_id:    os.Getenv("tenent_id"),
		client_id:    os.Getenv("client_id"),
		redirect_url: os.Getenv("redirect_url"),
		// client_secret: os.Getenv("client_secret"),
		scope: os.Getenv("scope"),

		// access_token
		// expires_time
		refresh_token: os.Getenv("refresh_token"),
	}

	fmt.Println(agent) // pass

	err := agent.RenewToken()
	if err != nil {
		log.Println(err)
	}
	fmt.Println(agent) // pass

}

func TestUpload(t *testing.T) {
	skipIfNoCreds(t)
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
	r, err := agent.Upload("image/jpeg", "", f)
	fmt.Println(err)
	fmt.Println(r)
}

func TestGet(t *testing.T) {
	skipIfNoCreds(t)
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
	r, _, _, _ := agent.Get(id, "")
	readerSaveToFile(r)
}

func TestDelete(t *testing.T) {
	skipIfNoCreds(t)
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
	mime := []string{
		"audio/x-m4a",
		"audio/mpeg",
	}
	{
		s := contentTypeToExtend("image/png")
		log.Println(s)
	}
	{
		s := contentTypeToExtend("application/x-msdownload")
		log.Println(s)
	}
	for _, s := range mime {
		s = contentTypeToExtend(s)
		log.Println(s)
	}
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
	tok, _ := m[`access_token`].(string)
	return tok
}

func skipIfNoCreds(t *testing.T) {
	t.Helper()
	if os.Getenv("tenent_id") == "" || accessToken() == "" {
		t.Skip("no Azure credentials, skipping integration test")
	}
}

func readerSaveToFile(reader io.Reader) {
	f, _ := os.Create("out.jpg") // 可以create同一个文件名的
	w := bufio.NewWriter(f)
	io.Copy(w, reader)
}

func TestMime(t *testing.T) {
	s := contentTypeToExtend("image/jpeg")
	fmt.Println(s)
}

func TestCreateSession(t *testing.T) {
	skipIfNoCreds(t)
	// godotenv.Load("access_token")
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

	// err := agent.RenewToken()
	// if err != nil {
	// 	log.Println(err)
	// }

	debug.I("agent", agent)
	// str := readFileToString("1.jpg")
	f, _ := os.Open("1.jpg")
	r, err := agent.CreateUploadSession("image/jpeg", f)
	fmt.Println(err)
	fmt.Println("result:", r)
}

func TestAgainMINE(t *testing.T) {
	a := contentTypeToExtend("image/webp")
	fmt.Println(a)
}

func TestSaveAccessToken(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/access"

	agt := &Agent{access_token: "test-access-token-value"}
	godotenv.Write(map[string]string{"access_token": agt.access_token}, filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read access file: %v", err)
	}
	if want := "access_token=\"test-access-token-value\"\n"; string(data) != want {
		t.Fatalf("expected %q, got %q", want, string(data))
	}
}

func TestSaveRefreshToken(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/refresh_token"

	agt := &Agent{refresh_token: "test-refresh-token-value"}
	godotenv.Write(map[string]string{"refresh_token": agt.refresh_token}, filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read refresh_token file: %v", err)
	}
	if want := "refresh_token=\"test-refresh-token-value\"\n"; string(data) != want {
		t.Fatalf("expected %q, got %q", want, string(data))
	}
}

func TestRenewTokenPersistence(t *testing.T) {
	skipIfNoCreds(t)

	origAccess := readFileIfExists("access_token")
	origRefresh := readFileIfExists("refresh_token")

	defer func() {
		if origAccess != "" {
			os.WriteFile("access_token", []byte(origAccess), 0644)
		} else {
			os.Remove("access_token")
		}
		if origRefresh != "" {
			os.WriteFile("refresh_token", []byte(origRefresh), 0644)
		}
	}()

	godotenv.Load("refresh_token")
	agent := Agent{
		tenent_id:     os.Getenv("tenent_id"),
		client_id:     os.Getenv("client_id"),
		redirect_url:  os.Getenv("redirect_url"),
		client_secret: os.Getenv("client_secret"),
		scope:         os.Getenv("scope"),
		access_token:  accessToken(),
		refresh_token: os.Getenv("refresh_token"),
		SALT:          os.Getenv("SALT"),
	}

	err := agent.RenewToken()
	if err != nil {
		t.Fatalf("RenewToken failed: %v", err)
	}

	// Verify access file content
	data, err := os.ReadFile("access_token")
	if err != nil {
		t.Fatal("access file not created after refresh")
	}
	envMap, _ := godotenv.Unmarshal(string(data))
	if got := envMap["access_token"]; got == "" {
		t.Fatal("access_token not persisted to access file after refresh")
	} else if got != agent.access_token {
		t.Fatalf("access_token mismatch in file: %s vs %s", got, agent.access_token)
	}

	// Verify refresh_token file content
	data, err = os.ReadFile("refresh_token")
	if err != nil {
		t.Fatal("refresh_token file not created after refresh")
	}
	envMap, _ = godotenv.Unmarshal(string(data))
	if got := envMap["refresh_token"]; got == "" {
		t.Fatal("refresh_token not persisted after refresh")
	} else if got != agent.refresh_token {
		t.Fatalf("refresh_token mismatch in file: %s vs %s", got, agent.refresh_token)
	}

	// Verify last_result.json
	if _, err := os.Stat("last_result.json"); os.IsNotExist(err) {
		t.Fatal("last_result.json not created after refresh")
	}
}

func readFileIfExists(name string) string {
	data, err := os.ReadFile(name)
	if err != nil {
		return ""
	}
	return string(data)
}
