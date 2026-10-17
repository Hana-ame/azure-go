package p_sda1_dev

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
)

// extractBody returns the payload that must actually be stored, plus the
// Content-Type to send it under.
//
// Why this exists: upload_external_noform stores the request body verbatim, so
// it must receive the file itself — never a multipart envelope. This driver
// used to fabricate that envelope with CreateFormFile("image", ...), which is
// how the envelope got stored in the first place. This is the last line of
// defence against it coming back through the front door: if a caller still
// hands us a multipart body, the file part is located and only its bytes are
// returned.
//
// Non-multipart bodies stream through untouched — no buffering, so the
// chunked-transfer path in Put keeps working and large uploads never land in
// memory. Only a declared multipart body is read for parsing.
func extractBody(contentType string, body io.Reader) (io.Reader, string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" {
		return body, normaliseContentType(contentType), nil
	}

	boundary := params["boundary"]
	if boundary == "" {
		// Declared multipart but no boundary we can parse with. Passing it
		// through is the safer guess than inventing an envelope layout.
		return body, normaliseContentType(contentType), nil
	}

	mr := multipart.NewReader(body, boundary)
	var (
		best     []byte
		bestCT   string
		bestName string
		bestFile string
	)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("p.sda1.dev parse multipart: %w", err)
		}

		b, rerr := io.ReadAll(part)
		part.Close()
		if rerr != nil {
			return nil, "", fmt.Errorf("p.sda1.dev read multipart part: %w", rerr)
		}
		if len(b) == 0 {
			continue
		}

		if preferPart(best, bestName, bestFile, part) {
			best = b
			bestCT = part.Header.Get("Content-Type")
			bestName = part.FormName()
			bestFile = part.FileName()
		}
	}

	if best == nil {
		return nil, "", errors.New("p.sda1.dev: no file part found in multipart body")
	}
	return bytes.NewReader(best), normaliseContentType(bestCT), nil
}

// preferPart reports whether candidate beats kept. A part carrying a filename
// beats one without; failing that, a part named "image" beats any other name;
// failing that, the first part wins.
func preferPart(kept []byte, keptName, keptFile string, candidate *multipart.Part) bool {
	if kept == nil {
		return true
	}
	candFile := candidate.FileName()
	if (candFile != "") != (keptFile != "") {
		return candFile != ""
	}
	if candidate.FormName() == "image" {
		return keptName != "image"
	}
	return false
}

// maxEnvelopeLayers bounds recursive stripping so a pathological object cannot
// loop. Objects were wrapped at most once in practice.
const maxEnvelopeLayers = 3

// StripEnvelope removes multipart/form-data envelope layers from data and
// returns the clean inner bytes. It stops at the first layer whose result is no
// longer an envelope, and never goes deeper than maxEnvelopeLayers.
//
// This repairs objects written before Put stopped fabricating envelopes: they
// hold the envelope instead of the image, and p.sda1.dev has no delete API, so
// re-uploading is the only alternative. Objects written by the fixed driver
// start with image magic bytes, so this is a no-op for them — it only ever
// fires on the legacy corpus.
func StripEnvelope(data []byte) ([]byte, bool) {
	out := data
	stripped := false
	for i := 0; i < maxEnvelopeLayers; i++ {
		next, ok := stripOneEnvelope(out)
		if !ok {
			break
		}
		out = next
		stripped = true
	}
	return out, stripped
}

// stripOneEnvelope removes a single multipart/form-data envelope layer.
//
// A multipart.Writer envelope begins with "--<boundary>\r\n", so the boundary
// is recovered from the first line and the body is decoded with the standard
// library rather than sliced by hand. Slicing on "\r\n--" would be wrong:
// binary image data may itself contain that sequence, so an envelope is only
// recognised by its *first* line.
func stripOneEnvelope(data []byte) ([]byte, bool) {
	if len(data) < 8 || !bytes.HasPrefix(data, []byte("--")) {
		return data, false
	}
	eol := bytes.Index(data, []byte("\r\n"))
	if eol < 3 {
		return data, false
	}
	boundary := string(data[2:eol])

	part, err := multipart.NewReader(bytes.NewReader(data), boundary).NextPart()
	if err != nil || part == nil {
		return data, false
	}
	defer part.Close()

	b, err := io.ReadAll(part)
	if err != nil || len(b) == 0 {
		return data, false
	}
	return b, true
}

// stripEnvelopeIfPresent removes a leading multipart envelope from body,
// repairing objects the old driver stored by accident. declaredLength is the
// Content-Length the upstream reported; it is returned unchanged when nothing
// was stripped.
//
// Only the first two bytes are read to decide whether the body can be an
// envelope at all. Everything the fixed driver ever wrote starts with image
// magic bytes, so those stream through to the caller untouched and are never
// buffered — the memory behaviour of a normal GET is unchanged. A body that
// really does start with "--" is read in full, because the boundary has to be
// parsed; in practice that is one legacy object of image size at a time.
func stripEnvelopeIfPresent(body io.ReadCloser, declaredLength int64) (io.ReadCloser, int64, bool, error) {
	prefix := make([]byte, 2)
	n, _ := io.ReadFull(body, prefix)
	if n < 2 || prefix[0] != '-' || prefix[1] != '-' {
		return io.NopCloser(io.MultiReader(bytes.NewReader(prefix[:n]), body)), declaredLength, false, nil
	}

	all, rerr := io.ReadAll(io.MultiReader(bytes.NewReader(prefix), body))
	if cerr := body.Close(); cerr != nil && rerr == nil {
		rerr = cerr
	}
	if rerr != nil {
		return nil, 0, false, rerr
	}
	if clean, ok := StripEnvelope(all); ok {
		return io.NopCloser(bytes.NewReader(clean)), int64(len(clean)), true, nil
	}
	return io.NopCloser(bytes.NewReader(all)), int64(len(all)), false, nil
}

// normaliseContentType returns contentType, falling back to
// application/octet-stream when empty. Never leave this unset on the outgoing
// request: net/http would sniff the first 512 bytes to decide. A multipart
// Content-Type is downgraded too — it must never leave the process alongside
// raw body bytes, or the header would lie about what it labels.
func normaliseContentType(contentType string) string {
	if mt, _, _ := mime.ParseMediaType(contentType); mt == "multipart/form-data" {
		return "application/octet-stream"
	}
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}
