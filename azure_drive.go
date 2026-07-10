package main

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"log"

	"github.com/Hana-ame/azure-go/Tools/debug"
	"github.com/Hana-ame/azure-go/Tools/orderedmap"
	"github.com/Hana-ame/azure-go/myfetch"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

const graphRoot = "https://graph.microsoft.com/v1.0"

type tokenResponse struct {
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	ExtExpiresIn int    `json:"ext_expires_in"`
	AccessToken  string `json:"access_token"`
}

// 作为使用api的代理
type Agent struct {
	tenent_id     string
	client_id     string
	redirect_url  string
	client_secret string
	scope         string

	expires_time  int64
	access_token  string
	refresh_token string

	SALT    string
	user_id string
}

func (agt *Agent) driveRoot() string {
	return graphRoot + "/users/" + agt.user_id + "/drive"
}

// 方法
// - Upload() 上传单张图片
// - Read() 得到单张图片的 302 Redirect
// - Renewoken() 更新Token
/// TODO：如何获取Token, 因为忘记了, 似乎要上blade去重新弄。两年一次。下次需要做好笔记。
/// 记得原来的笔记是在旧电脑上

func (agt *Agent) RenewToken() error {
	endpoint := `https://login.microsoftonline.com/` + agt.tenent_id + `/oauth2/v2.0/token`

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", agt.client_id)
	form.Set("client_secret", agt.client_secret)
	form.Set("scope", "https://graph.microsoft.com/.default")

	log.Printf("[TokenRefresh] endpoint: %s", endpoint)
	log.Printf("[TokenRefresh] using client_credentials, client_id=%s", agt.client_id)

	body := strings.NewReader(form.Encode())
	resp, err := myfetch.Fetch(
		http.MethodPost,
		endpoint,
		map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		body,
	)
	if err != nil {
		log.Printf("[TokenRefresh] HTTP request failed: %v", err)
		return err
	}
	log.Printf("[TokenRefresh] HTTP status: %d", resp.StatusCode)

	bodyBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	log.Printf("[TokenRefresh] Raw response body: %s", string(bodyBytes))

	var tr tokenResponse
	if err := json.Unmarshal(bodyBytes, &tr); err != nil {
		log.Printf("[TokenRefresh] JSON decode failed: %v", err)
		return err
	}

	log.Printf("[TokenRefresh] access_token length: %d, expires_in: %d", len(tr.AccessToken), tr.ExpiresIn)

	if tr.AccessToken == "" {
		log.Printf("[TokenRefresh] WARN: access_token empty in response")
		return fmt.Errorf("empty access_token in response")
	}

	oldAccessToken := agt.access_token
	agt.access_token = tr.AccessToken
	agt.expires_time = time.Now().Unix() + int64(tr.ExpiresIn)

	log.Printf("[TokenRefresh] access_token changed: length old=%d new=%d", len(oldAccessToken), len(agt.access_token))
	log.Printf("[TokenRefresh] new expires_time: %d (unix), expires_in: %d minutes",
		agt.expires_time, (agt.expires_time-time.Now().Unix())/60)

	godotenv.Write(map[string]string{"access_token": agt.access_token}, "access_token")
	return nil
}

// image/png, image/jpeg
//
// return an Object
// find what need in (Object).id
func (agt *Agent) Upload(ContentType, ContentLength string, body io.Reader) (*orderedmap.OrderedMap, error) {
	// save img to `<timestamp>-<randomString>`
	endpoint := agt.driveRoot() + `/root:/img/` +
		strconv.Itoa(int(time.Now().Unix())) + `-` +
		uuid.New().String()[:8] + contentTypeToExtend(ContentType) + `:/content`

	resp, err := myfetch.Fetch(
		http.MethodPut,
		endpoint,
		map[string]string{
			// "Origin":         os.Getenv("origin"),
			"Authorization":  "Bearer " + agt.access_token,
			"Content-Type":   ContentType,
			"Content-Length": ContentLength,
		},
		body,
	)
	if err != nil {
		return nil, err
	}

	result, err := myfetch.ResponseToJson(resp)
	if err != nil {
		return result, err
	}

	return result, nil
}

// TODO: add cache for deleted pictures.
// get picture body reader
func (agt *Agent) Get(id, fn string) (file io.ReadCloser, contentLength int64, contentType string, err error) {
	endpoint := agt.driveRoot() + `/items/` + id + `/content`

	resp, err := myfetch.Fetch(
		http.MethodGet,
		endpoint,
		map[string]string{
			// "Origin":        os.Getenv("origin"),
			"Authorization": "Bearer " + agt.access_token,
		},
		nil,
	)
	if err != nil {
		return
	}
	file = resp.Body
	contentLength = resp.ContentLength
	contentType = resp.Header.Get("Content-Type")

	return
}

// delete picture
func (agt *Agent) Delete(id string) (*orderedmap.OrderedMap, error) {
	endpoint := agt.driveRoot() + `/items/` + id

	resp, err := myfetch.Fetch(
		http.MethodDelete,
		endpoint,
		map[string]string{
			// "Origin":        os.Getenv("origin"),
			"Authorization": "Bearer " + agt.access_token,
		},
		nil,
	)
	if err != nil {
		return nil, err
	}

	result, err := myfetch.ResponseToJson(resp)
	if err != nil {
		return result, err
	}
	// fmt.Println(resp)
	// fmt.Println(resp.Ctx().Response)

	return result, nil
}

func (agt *Agent) CreateUploadSession(ContentType string, body io.Reader) (*orderedmap.OrderedMap, error) {
	endpoint := agt.driveRoot() + `/root:/large/` +
		strconv.Itoa(int(time.Now().Unix())) + `-` +
		uuid.New().String()[:8] + contentTypeToExtend(ContentType) + `:/createUploadSession`

	resp, err := myfetch.Fetch(
		http.MethodPost,
		endpoint,
		map[string]string{
			// "Origin":        os.Getenv("origin"),
			"Authorization": "Bearer " + agt.access_token,
		},
		nil,
	)
	if err != nil {
		return nil, err
	}

	result, err := myfetch.ResponseToJson(resp)
	if err != nil {
		return result, err
	}
	// fmt.Println(resp)
	// fmt.Println(resp.Ctx().Response)

	return result, nil
}

var KEYS = []string{
	".jpg",
	".png",
	".gif",
	".webp",
	".bmp",
	// mp3 should be first.
	".mp3",
	".mp4",
	".mkv",
	".webm",
}

func contentTypeToExtend(mimeType string) string {
	extensions, err := mime.ExtensionsByType(mimeType)
	debug.I("mime", extensions)
	if err != nil || len(extensions) == 0 {
		return ""
	}
	ext := pickSurfix(extensions, KEYS)
	return ext
}

func pickSurfix(arr []string, keys []string) string {
	for _, key := range keys {
		for _, typ := range arr {
			if typ == key {
				return typ
			}
		}
	}
	return arr[0]
}

// --- StorageDriver interface implementation ---

func (agt *Agent) Put(path, contentType string, body io.Reader) (string, error) {
	endpoint := agt.driveRoot() + `/root:/` + path + `:/content`

	resp, err := myfetch.Fetch(
		http.MethodPut,
		endpoint,
		map[string]string{
			"Authorization": "Bearer " + agt.access_token,
			"Content-Type":  contentType,
		},
		body,
	)
	if err != nil {
		return "", err
	}

	result, err := myfetch.ResponseToJson(resp)
	if err != nil {
		return "", err
	}
	return result.GetOrDefault("id", "").(string), nil
}

func (agt *Agent) GetByPath(path string) (io.ReadCloser, int64, string, error) {
	endpoint := agt.driveRoot() + `/root:/` + path + `:/content`

	resp, err := myfetch.Fetch(
		http.MethodGet,
		endpoint,
		map[string]string{
			"Authorization": "Bearer " + agt.access_token,
		},
		nil,
	)
	if err != nil {
		return nil, 0, "", err
	}
	return resp.Body, resp.ContentLength, resp.Header.Get("Content-Type"), nil
}

func (agt *Agent) DeleteByPath(path string) error {
	endpoint := agt.driveRoot() + `/root:/` + path

	_, err := myfetch.Fetch(
		http.MethodDelete,
		endpoint,
		map[string]string{
			"Authorization": "Bearer " + agt.access_token,
		},
		nil,
	)
	return err
}

// head picture by ID
func (agt *Agent) HeadByID(id string) (contentLength int64, contentType string, err error) {
	endpoint := agt.driveRoot() + `/items/` + id + `/content`

	resp, err := myfetch.Fetch(
		http.MethodHead,
		endpoint,
		map[string]string{
			"Authorization": "Bearer " + agt.access_token,
		},
		nil,
	)
	if err != nil {
		return 0, "", err
	}
	return resp.ContentLength, resp.Header.Get("Content-Type"), nil
}

func (agt *Agent) HeadByPath(path string) (int64, string, error) {

	endpoint := agt.driveRoot() + `/root:/` + path + `:/content`

	resp, err := myfetch.Fetch(
		http.MethodHead,
		endpoint,
		map[string]string{
			"Authorization": "Bearer " + agt.access_token,
		},
		nil,
	)
	if err != nil {
		return 0, "", err
	}
	return resp.ContentLength, resp.Header.Get("Content-Type"), nil
}
