package main

import (
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Hana-ame/azure-go/Tools/orderedmap"
	"github.com/Hana-ame/azure-go/myfetch"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

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

	SALT string
}

// 方法
// - Upload() 上传单张图片
// - Read() 得到单张图片的 302 Redirect
// - Renewoken() 更新Token
/// TODO：如何获取Token, 因为忘记了, 似乎要上blade去重新弄。两年一次。下次需要做好笔记。
/// 记得原来的笔记是在旧电脑上

// renew the token
// save the newest `refresh_token` to `./refresh_token`
func (agt *Agent) RenewToken() error {
	endpoint := `https://login.microsoftonline.com/` + agt.tenent_id + `/oauth2/v2.0/token`

	body, _ := myfetch.URLEncodedFormReader(map[string]string{
		"client_id":     agt.client_id,
		"refresh_token": agt.refresh_token,
		"grant_type":    "refresh_token",
		// "client_secret": agt.client_secret,
	})
	resp, err := myfetch.Fetch(
		http.MethodPost,
		endpoint,
		map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
			"Origin":       os.Getenv("origin")},
		body,
	)
	if err != nil {
		return err
	}

	result, err := myfetch.ResponseToJson(resp)
	if err != nil {
		return err
	}
	// ************************
	// 抓到数据之后从里面提取出来,为了测试需要把现有的client_id云云从cloudflare上面抓下来
	// ************************

	// log.Println(result) // ok
	// renew agent
	agt.access_token = result.GetOrDefault(`access_token`, agt.access_token).(string)
	agt.refresh_token = result.GetOrDefault(`refresh_token`, agt.refresh_token).(string)
	agt.expires_time = time.Now().Unix() + int64(result.GetOrDefault(`expires_in`, float64(agt.expires_time)).(float64))

	// save to file
	godotenv.Write(map[string]string{"refresh_token": agt.refresh_token}, "refresh_token")

	return nil
}

// image/png, image/jpeg
//
// return an Object
// find what need in (Object).id
func (agt *Agent) Upload(ContentType, ContentLength string, body io.Reader) (*orderedmap.OrderedMap, error) {
	// save img to `<timestamp>-<randomString>`
	endpoint := `https://graph.microsoft.com/v1.0/me/drive/root:/img/` +
		strconv.Itoa(int(time.Now().Unix())) + `-` +
		uuid.New().String()[:8] + contentTypeToExtend(ContentType) + `:/content`

	resp, err := myfetch.Fetch(
		http.MethodPut,
		endpoint,
		map[string]string{
			"Origin":         os.Getenv("origin"),
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
func (agt *Agent) Get(id string) (file io.ReadCloser, contentLength int64, contentType string, err error) {
	endpoint := `https://graph.microsoft.com/v1.0/me/drive/items/` + id + `/content`

	resp, err := myfetch.Fetch(
		http.MethodGet,
		endpoint,
		map[string]string{
			"Origin":        os.Getenv("origin"),
			"Authorization": "Bearer " + agt.access_token,
		},
		nil,
	)
	if err != nil {
		return
	}

	contentLength = resp.ContentLength
	contentType = resp.Header.Get("Content-Type")

	file = resp.Body
	return
}

// delete picture
func (agt *Agent) Delete(id string) (*orderedmap.OrderedMap, error) {
	endpoint := `https://graph.microsoft.com/v1.0/me/drive/items/` + id

	resp, err := myfetch.Fetch(
		http.MethodDelete,
		endpoint,
		map[string]string{
			"Origin":        os.Getenv("origin"),
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
