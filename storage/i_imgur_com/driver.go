// Package i_imgur_com implements an imgur.com image-hosting backend that uses the
// public web client-id (no registration or OAuth required).
//
// Unlike p_inari_site, this backend needs no session cookie or CSRF token:
// the web client-id is a static credential. The transport honours
// HTTP_PROXY / HTTPS_PROXY (ProxyFromEnvironment) so a CN deployment can route
// out through a proxy; imgur is NOT behind a CN-blocked Cloudflare edge the
// way up.inari.site is.
package i_imgur_com

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hana-ame/azure-go/storage"
)

// ImgurWebClientID is imgur.com's built-in public web client-id. It ships in
// the imgur web page and is used for anonymous uploads without registration.
const ImgurWebClientID = "546c25a59c58ad7"

// ImgurUploadURL is the anonymous image-upload endpoint.
const ImgurUploadURL = "https://api.imgur.com/3/image?client_id=" + ImgurWebClientID

// ImgurPublicBase is where imgur serves uploaded images.
const ImgurPublicBase = "https://i.imgur.com"

const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// imgurResponse is the imgur v3 JSON envelope. Only the fields the driver
// needs are decoded.
type imgurResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Link       string `json:"link"`
		Deletehash string `json:"deletehash"`
	} `json:"data"`
}

// Driver implements storage.StorageDriver for imgur.com using the public web
// client-id. It accepts both a raw image body and a multipart body carrying a
// file part, re-encapsulates it as multipart field "image" plus "type=file",
// and forwards to imgur with browser-like headers.
type Driver struct {
	uploadURL  string
	publicBase string
	client     *http.Client
}

// New creates a Driver pointed at imgur's production endpoint.
func New() *Driver {
	return &Driver{
		uploadURL:  ImgurUploadURL,
		publicBase: ImgurPublicBase,
		client: &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				// Respect HTTP_PROXY / HTTPS_PROXY so a CN deployment can
				// route out through a proxy (imgur is otherwise unreachable
				// from mainland China without one).
				Proxy: http.ProxyFromEnvironment,
			},
		},
	}
}

// Put uploads image data to imgur. It accepts either a raw body (the bytes
// are the image) or a multipart body carrying a file part named "image"
// (falling back to "file"), re-encapsulates it as multipart field "image" plus
// "type=file", and forwards to imgur.
//
// The returned Path is the imgur image id; URL is the direct i.imgur.com link.
func (d *Driver) Put(path, contentType string, body io.Reader) (*storage.PutResult, error) {
	data, filename, err := d.extractBody(contentType, body, path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("imgur: empty image data")
	}

	// Re-encapsulate as multipart field "image" (imgur's field name) plus the
	// web page's "type=file".
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("image", filename)
	if err != nil {
		return nil, fmt.Errorf("imgur: create form: %w", err)
	}
	if _, err := fw.Write(data); err != nil {
		return nil, fmt.Errorf("imgur: write form: %w", err)
	}
	if err := mw.WriteField("type", "file"); err != nil {
		return nil, fmt.Errorf("imgur: write type field: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("imgur: close form: %w", err)
	}

	req, err := http.NewRequest("POST", d.uploadURL, &buf)
	if err != nil {
		return nil, fmt.Errorf("imgur: create req: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://imgur.com")
	req.Header.Set("Referer", "https://imgur.com/upload")
	req.Header.Set("User-Agent", browserUA)

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("imgur: post: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("imgur: read resp: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("imgur: api responded %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var apiResp imgurResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("imgur: decode: %w", err)
	}
	if !apiResp.Success || apiResp.Data.Link == "" {
		return nil, fmt.Errorf("imgur: upload failed: %s", strings.TrimSpace(string(respBody)))
	}

	link := apiResp.Data.Link
	id := imgurIDFromLink(link)
	// Path keeps the extension (e.g. "SmWnjfK.png") so the controller can
	// emit /i.imgur.com/<path> and the proxy Get can fetch the exact file.
	imgurPath := imgurPathFromLink(link)
	deleteURL := ""
	if apiResp.Data.Deletehash != "" {
		deleteURL = "https://api.imgur.com/3/image/" + apiResp.Data.Deletehash + "?client_id=" + ImgurWebClientID
	}
	return &storage.PutResult{
		ID:        id,
		Path:      imgurPath,
		URL:       link,
		DeleteURL: deleteURL,
	}, nil
}

// extractBody returns the raw image bytes and a filename hint, supporting both
// raw and multipart request bodies. For a multipart body the boundary is read
// from contentType and the file part named "image" (fallback "file") extracted.
func (d *Driver) extractBody(contentType string, body io.Reader, pathHint string) ([]byte, string, error) {
	filename := filepath.Base(strings.TrimRight(pathHint, "/"))
	if filename == "" || filename == "." || filename == "/" {
		filename = "upload.png"
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err == nil && mediaType == "multipart/form-data" {
		boundary := params["boundary"]
		if boundary == "" {
			return nil, "", fmt.Errorf("imgur: multipart body without boundary")
		}
		mr := multipart.NewReader(body, boundary)
		var (
			best     []byte
			bestName string
		)
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, "", fmt.Errorf("imgur: parse multipart: %w", err)
			}
			pb, rerr := io.ReadAll(part)
			part.Close()
			if rerr != nil {
				return nil, "", fmt.Errorf("imgur: read multipart part: %w", rerr)
			}
			if len(pb) == 0 {
				continue
			}
			name := part.FormName()
			// Prefer a part named "image", then "file", else first non-empty.
			if best == nil {
				best = pb
				bestName = part.FileName()
			} else if name == "image" {
				best = pb
				bestName = part.FileName()
				break
			} else if name == "file" && bestName == "" {
				best = pb
				bestName = part.FileName()
			}
		}
		if best == nil {
			return nil, "", fmt.Errorf("imgur: no image part in multipart body")
		}
		if bestName != "" {
			filename = filepath.Base(bestName)
		}
		return best, filename, nil
	}

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, "", fmt.Errorf("imgur: read raw body: %w", err)
	}
	return data, filename, nil
}

// imgurIDFromLink extracts the image id from an i.imgur.com URL. Imgur links
// can carry an extension (a.png) or not; the id is the last path segment
// without the extension.
func imgurIDFromLink(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	seg := path.Base(u.Path)
	if ext := path.Ext(seg); ext != "" {
		seg = strings.TrimSuffix(seg, ext)
	}
	return seg
}

// imgurPathFromLink returns the full last path segment of an i.imgur.com link,
// including any extension (e.g. "SmWnjfK.png"). When the link has no
// extension, the id alone is returned. This is the value the controller uses
// for /i.imgur.com/<path> so the proxy Get fetches the exact file.
func imgurPathFromLink(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	trimmed := strings.TrimRight(u.Path, "/")
	seg := path.Base(trimmed)
	if seg == "" || seg == "." || seg == "/" {
		return ""
	}
	return seg
}

// Get streams the image bytes from the i.imgur.com public host so the
// controller can serve it through its own proxy route (/i.imgur.com/*path).
// path is the full filename (e.g. "SmWnjfK.png") or the bare id.
func (d *Driver) Get(path string) (io.ReadCloser, int64, string, error) {
	// Rebuild the direct URL. The public host is https://i.imgur.com, and the
	// path is either "SmWnjfK.png" or "SmWnjfK" — both work on i.imgur.com.
	u := d.publicBase + "/" + strings.TrimLeft(path, "/")
	resp, err := d.client.Get(u)
	if err != nil {
		return nil, 0, "", fmt.Errorf("imgur: get: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, "", fmt.Errorf("imgur: get: %s", resp.Status)
	}
	return resp.Body, resp.ContentLength, resp.Header.Get("Content-Type"), nil
}

// Delete is not supported through this driver: the upload response carries a
// deletehash, but there is no delete-by-path API in the StorageDriver contract.
func (d *Driver) Delete(path string) error {
	return fmt.Errorf("imgur: delete not supported")
}

// Head returns metadata for an image on the i.imgur.com public host.
func (d *Driver) Head(path string) (int64, string, error) {
	u := d.publicBase + "/" + strings.TrimLeft(path, "/")
	resp, err := d.client.Head(u)
	if err != nil {
		return 0, "", fmt.Errorf("imgur: head: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("imgur: head: %s", resp.Status)
	}
	return resp.ContentLength, resp.Header.Get("Content-Type"), nil
}
