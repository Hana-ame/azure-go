package main

import (
	"errors"
	"io"
	"log"
	"strconv"
	"time"

	"github.com/Hana-ame/request"
	"github.com/google/uuid"
	"github.com/iancoleman/orderedmap"
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
}

// 方法
// - Upload() 上传单张图片
// - Read() 得到单张图片的 302 Redirect
// - Renewoken() 更新Token
/// TODO：如何获取Token, 因为忘记了, 似乎要上blade去重新弄。两年一次。下次需要做好笔记。
/// 记得原来的笔记是在旧电脑上

func (agt *Agent) RenewToken() error {
	endpoint := `https://login.microsoftonline.com/` + agt.tenent_id + `/oauth2/v2.0/token`
	// endpoint = `https://moonchan.xyz/api-pack/echo`

	var result map[string]any

	c := request.Client{
		URL:    endpoint,
		Method: "POST",
		Header: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		URLEncodedForm: map[string]string{
			"client_id":     agt.client_id,
			"refresh_token": agt.refresh_token,
			"grant_type":    "refresh_token",
			"client_secret": agt.client_secret,
		},
	}
	resp := c.Send()
	resp.Scan(&result)

	if !resp.OK() {
		// handle error
		log.Println(resp.Error())
		return resp.Error()
	}

	// str := resp.String()
	// log.Println(str)

	// bytes := resp.Bytes()

	// ************************
	// 抓到数据之后从里面提取出来,为了测试需要把现有的client_id云云从cloudflare上面抓下来
	// ************************

	// log.Println(result) // ok
	// renew agent
	agt.access_token = result[`access_token`].(string)
	agt.refresh_token = result[`refresh_token`].(string)
	agt.expires_time = time.Now().Unix() + int64(result[`expires_in`].(float64))

	// save to file
	godotenv.Write(map[string]string{"refresh_token": agt.refresh_token}, "refresh_token")

	return nil
}

// image/png, image/jpeg
//
// return
// map.id
func (agt *Agent) Upload(ContentType string, body string) (*orderedmap.OrderedMap, error) {
	endpoint := `https://graph.microsoft.com/v1.0/me/drive/root:/img/` +
		strconv.Itoa(int(time.Now().Unix())) + `-` +
		uuid.New().String()[:8] + contentTypeToExtend(ContentType) + `:/content`
	// endpoint = "https://moonchan.xyz/api-pack/echo"

	result := orderedmap.New()

	c := request.Client{
		URL:    endpoint,
		Method: "PUT",
		Header: map[string]string{
			"Content-Type": ContentType,
		},
		Bearer: agt.access_token,
		String: body,
	}
	resp := c.Send()

	resp.Scan(&result)
	if !resp.OK() {
		log.Println(resp.Error())
		return result, resp.Error()
	}

	return result, nil

}

// forced follow
func (agt *Agent) Get(id string) (io.ReadCloser, error) {
	endpoint := `https://graph.microsoft.com/v1.0/me/drive/items/` + id + `/content`
	// endpoint = "https://moonchan.xyz/api-pack/echo"

	c := request.Client{
		URL:    endpoint,
		Method: "GET",
		Bearer: agt.access_token,
	}

	resp := c.Resp().DoNotRead()
	body := resp.Body()
	if resp.Code() != 200 {
		return body, errors.New(strconv.Itoa(resp.Code()))
	}

	return body, nil
}

// forced follow
func (agt *Agent) Delete(id string) (*orderedmap.OrderedMap, error) {
	endpoint := `https://graph.microsoft.com/v1.0/me/drive/items/` + id
	// endpoint = "https://moonchan.xyz/api-pack/echo"

	c := request.Client{
		URL:    endpoint,
		Method: "DELETE",
		Bearer: agt.access_token,
	}

	result := orderedmap.New()

	resp := c.Send().Scan(&result)
	if !resp.OK() {
		return result, resp.Error()
	}

	// fmt.Println(resp)
	// fmt.Println(resp.Ctx().Response)

	return result, nil
}

func contentTypeToExtend(ct string) string {
	switch ct {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	}
	return ""
}
