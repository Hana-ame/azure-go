package p_inari_site

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Hana-ame/azure-go/storage"
)

// Upstream endpoints (Lsky Pro v2 at up.inari.site, guest upload).
const (
	// sessionTTLDefault is how long a fetched CSRF token + session cookie are
	// reused. The upstream cookie carries Max-Age=7200 (2 h); refreshing at 1 h
	// keeps well inside it, so a cached pair never hits the 419 boundary.
	sessionTTLDefault = time.Hour
	// readCap bounds how much of the homepage is parsed for the CSRF token.
	readCap = 1 << 20
)

// httpStatusExpired is Laravel's "419 Page Expired", returned when the
// X-CSRF-Token does not match the session cookie. It is not a standard
// http.Status constant, hence a named constant.
const httpStatusExpired = 419

// browserUA mimics a real browser so the Cloudflare edge does not fingerprint
// the request as a bot.
const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

var csrfTokenRe = regexp.MustCompile(`<meta\s+name="csrf-token"\s+content="([^"]+)"`)

// uploadResponse is the Lsky Pro v2 JSON envelope. Only the fields the driver
// needs are decoded; the rest is ignored.
type uploadResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Pathname string `json:"pathname"`
		MD5      string `json:"md5"`
		Links    struct {
			URL string `json:"url"`
		} `json:"links"`
	} `json:"data"`
}

// sessionState is the cached credential pair. csrfToken and the cookie in the
// jar always come from the same GET response, so they are paired by
// construction — never mix a fresh token with an old cookie.
type sessionState struct {
	csrfToken string
	fetchedAt time.Time
}

// Driver implements storage.StorageDriver for up.inari.site (Lsky Pro v2).
//
// Upload requires a matched lsky_pro_session cookie + X-CSRF-Token header
// (Laravel CSRF). Both are obtained from a single GET of the homepage and
// cached for sessionTTL; on expiry the pair is re-fetched. A 419 response
// (CSRF mismatch/expired) invalidates the cache and is retried once when the
// body can be rewound.
type Driver struct {
	baseURL       string // https://up.inari.site
	uploadURL     string // https://up.inari.site/upload
	publicBaseURL string // https://p.inari.site (public read host)

	sessionTTL time.Duration
	session    *http.Client // carries the cookie jar (lsky_pro_session)
	read       *http.Client // plain tcp4 client for public reads

	mu    sync.Mutex
	state sessionState
}

// New creates a Driver pointed at the production endpoints.
func New() *Driver {
	jar, _ := cookiejar.New(nil)
	return &Driver{
		baseURL:       "https://up.inari.site",
		uploadURL:     "https://up.inari.site/upload",
		publicBaseURL: "https://p.inari.site",
		sessionTTL:    sessionTTLDefault,
		session: &http.Client{
			Transport: newHTTPClient().Transport,
			Jar:       jar,
			Timeout:   120 * time.Second,
		},
		read: newReadClient(),
	}
}

// PublicURL returns the directly-reachable public URL for a stored object.
// Lsky Pro serves guest uploads under /guest/<pathname> on p.inari.site.
func (d *Driver) PublicURL(path string) string {
	return d.publicBaseURL + "/guest/" + strings.TrimPrefix(path, "/")
}

// ensureSession returns a usable CSRF token, re-fetching the homepage (which
// also refreshes the session cookie in the jar) when the cache is empty or
// older than sessionTTL.
func (d *Driver) ensureSession() (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.csrfToken != "" && time.Since(d.state.fetchedAt) < d.sessionTTL {
		return d.state.csrfToken, nil
	}
	return d.fetchSessionLocked()
}

// fetchSessionLocked performs the GET that both sets the session cookie (into
// the jar) and yields the CSRF token. It must be a single response: the token
// and the cookie are a matched pair and cannot come from separate requests.
// Caller holds d.mu.
func (d *Driver) fetchSessionLocked() (string, error) {
	req, err := http.NewRequest("GET", d.baseURL+"/", nil)
	if err != nil {
		return "", fmt.Errorf("p.inari.site session req: %w", err)
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := d.session.Do(req)
	if err != nil {
		return "", fmt.Errorf("p.inari.site session get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("p.inari.site session get responded %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, readCap))
	if err != nil {
		return "", fmt.Errorf("p.inari.site session read: %w", err)
	}
	m := csrfTokenRe.FindSubmatch(body)
	if len(m) < 2 || len(m[1]) == 0 {
		return "", fmt.Errorf("p.inari.site: no csrf-token meta in homepage")
	}
	token := string(m[1])
	d.state = sessionState{csrfToken: token, fetchedAt: time.Now()}
	return token, nil
}

// invalidateSession drops the cached pair so the next upload re-fetches it.
func (d *Driver) invalidateSession() {
	d.mu.Lock()
	d.state = sessionState{}
	d.mu.Unlock()
}

// Put uploads an image body to up.inari.site/upload as multipart field
// "file" (Lsky's field name), attaching the cached X-CSRF-Token and the
// session cookie.
//
// contentType tells us whether the body is raw image bytes or a multipart
// envelope. When the client POSTs multipart (frontend upload), the envelope
// is parsed and only the file part (preferring a part named "file", then
// "image") is forwarded — the envelope itself must never be wrapped as the
// file, or Lsky would store an envelope. Raw bodies stream through.
//
// The returned Path is the Lsky pathname (e.g. "26-11/08/<hash>.png"); the
// controller maps it to /p.inari.site/<pathname> and a public URL.
func (d *Driver) Put(path, contentType string, body io.Reader) (*storage.PutResult, error) {
	filename := filepath.Base(strings.TrimRight(path, "/"))
	if filename == "" || filename == "." || filename == "/" {
		filename = "file"
	}

	// If the caller handed us a multipart envelope, extract the file part so
	// we never upload an envelope as the image.
	body, contentType, partName, err := extractFilePart(contentType, body, filename)
	if err != nil {
		return nil, err
	}
	filename = partName

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			// Retry needs the body again; only a seekable reader can replay.
			s, ok := body.(io.ReadSeeker)
			if !ok {
				return nil, fmt.Errorf("p.inari.site session expired (419): %w; body not seekable, retry manually", lastErr)
			}
			if _, err := s.Seek(0, io.SeekStart); err != nil {
				return nil, fmt.Errorf("p.inari.site retry rewind: %w", err)
			}
		}

		token, err := d.ensureSession()
		if err != nil {
			return nil, err
		}

		result, status, respBody, err := d.uploadOnce(filename, token, body)
		if err != nil {
			return nil, err
		}
		if status == http.StatusOK {
			return result, nil
		}
		if status == httpStatusExpired {
			d.invalidateSession()
			lastErr = fmt.Errorf("p.inari.site session expired (419): %s", strings.TrimSpace(string(respBody)))
			continue
		}
		return nil, fmt.Errorf("p.inari.site api responded %d: %s", status, strings.TrimSpace(string(respBody)))
	}
	return nil, lastErr
}

// extractFilePart pulls the image bytes out of a multipart/form-data body,
// preferring a part named "file" then "image". A non-multipart body is
// returned unchanged (with its declared Content-Type and the default filename).
//
// The extracted bytes are wrapped in a bytes.Reader, which also makes the body
// seekable — the 419 retry path rewinds it. A raw body is returned as-is.
func extractFilePart(contentType string, body io.Reader, defaultName string) (io.Reader, string, string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" {
		return body, contentType, defaultName, nil
	}
	boundary := params["boundary"]
	if boundary == "" {
		return body, contentType, defaultName, nil
	}

	mr := multipart.NewReader(body, boundary)
	var (
		best     []byte
		bestFile string
	)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", "", fmt.Errorf("p.inari.site parse multipart: %w", err)
		}
		b, rerr := io.ReadAll(part)
		part.Close()
		if rerr != nil {
			return nil, "", "", fmt.Errorf("p.inari.site read multipart part: %w", rerr)
		}
		if len(b) == 0 {
			continue
		}
		name := part.FormName()
		file := part.FileName()
		// A filename-bearing part named "file" or "image" wins outright.
		if (name == "file" || name == "image") && file != "" {
			best = b
			bestFile = file
			break
		}
		if best == nil {
			best = b
			bestFile = file
		}
	}
	if best == nil {
		return nil, "", "", errors.New("p.inari.site: no file part found in multipart body")
	}
	if bestFile != "" {
		defaultName = filepath.Base(bestFile)
	}
	// Lsky re-sniffs the content type from the uploaded bytes; a neutral
	// Content-Type is safe here and never mislabels the body.
	return bytes.NewReader(best), "application/octet-stream", defaultName, nil
}

// uploadOnce performs a single POST of body as multipart field "file".
func (d *Driver) uploadOnce(filename, token string, body io.Reader) (*storage.PutResult, int, []byte, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	var copyErr error
	go func() {
		defer pw.Close()
		fw, err := mw.CreateFormFile("file", filename)
		if err != nil {
			copyErr = err
			return
		}
		if _, err := io.Copy(fw, body); err != nil {
			copyErr = err
			return
		}
		if err := mw.Close(); err != nil {
			copyErr = err
		}
	}()

	req, err := http.NewRequest("POST", d.uploadURL, pr)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("p.inari.site create req: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-CSRF-Token", token)
	req.Header.Set("Origin", d.baseURL)
	req.Header.Set("Referer", d.baseURL+"/")
	req.Header.Set("User-Agent", browserUA)

	resp, err := d.session.Do(req)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("p.inari.site post: %w", err)
	}
	defer resp.Body.Close()

	respBody, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return nil, 0, nil, fmt.Errorf("p.inari.site read resp: %w", rerr)
	}
	if copyErr != nil {
		return nil, 0, nil, fmt.Errorf("p.inari.site stream body: %w", copyErr)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, respBody, nil
	}

	var apiResp uploadResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, resp.StatusCode, respBody, fmt.Errorf("p.inari.site decode: %w", err)
	}
	if !apiResp.Status {
		return nil, resp.StatusCode, respBody, fmt.Errorf("p.inari.site upload failed: %s", apiResp.Message)
	}

	pathname := strings.TrimPrefix(apiResp.Data.Pathname, "/")
	if pathname == "" {
		// Fall back to the URL from links if pathname is unexpectedly empty.
		pathname = strings.TrimPrefix(apiResp.Data.Links.URL, d.publicBaseURL+"/guest/")
	}
	etag := ""
	if apiResp.Data.MD5 != "" {
		etag = `"` + apiResp.Data.MD5 + `"`
	}
	return &storage.PutResult{ID: pathname, Path: pathname, ETag: etag}, resp.StatusCode, respBody, nil
}

// Get streams the public object from p.inari.site/guest/<pathname>.
func (d *Driver) Get(path string) (io.ReadCloser, int64, string, error) {
	u := d.PublicURL(path)
	resp, err := d.read.Get(u)
	if err != nil {
		return nil, 0, "", fmt.Errorf("p.inari.site get: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, "", fmt.Errorf("p.inari.site get: %s", resp.Status)
	}
	return resp.Body, resp.ContentLength, resp.Header.Get("Content-Type"), nil
}

// Delete is not supported: Lsky Pro v2 guest uploads have no public delete
// API. Mirrors the p.sda1.dev driver.
func (d *Driver) Delete(path string) error {
	return fmt.Errorf("p.inari.site delete not supported")
}

// Head returns metadata for a public object.
func (d *Driver) Head(path string) (int64, string, error) {
	u := d.PublicURL(path)
	resp, err := d.read.Head(u)
	if err != nil {
		return 0, "", fmt.Errorf("p.inari.site head: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("p.inari.site head: %s", resp.Status)
	}
	return resp.ContentLength, resp.Header.Get("Content-Type"), nil
}
