package myfetch

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/Hana-ame/azure-go/Tools/orderedmap"
)

func NewReader(s any) io.Reader {
	switch v := s.(type) {
	case string:
		return strings.NewReader(v)
	case []byte:
		return bytes.NewReader(v)
	}
	return nil
}

type URLEncodedForm struct {
	data any
	// Reader() (io.Reader, error)
}

func (f *URLEncodedForm) Reader() (io.Reader, error) {
	buf := &bytes.Buffer{}

	switch bv := f.data.(type) {
	case string:
		buf.WriteString(bv)
	case []byte:
		buf.Write(bv)
	case map[string]string:
		data := make(url.Values)
		for k, v := range bv {
			data.Set(k, v)
		}
		buf.WriteString(data.Encode())
	case map[string][]string:
		buf.WriteString(url.Values(bv).Encode())
	case url.Values:
		buf.WriteString(bv.Encode())
	case *orderedmap.OrderedMap:
		data := make(url.Values)
		for _, k := range bv.Keys() {
			switch v, _ := bv.Get(k); sv := v.(type) {
			case string:
				data.Set(k, sv)
			case []string:
				data[k] = sv
			}
		}
		buf.WriteString(data.Encode())
	default:
		return buf, fmt.Errorf("unknown urlencoded type: %T", f.data)
	}

	return buf, nil
}

// Apply application/x-www-form-urlencoded
func URLEncodedFormReader(data any) (io.Reader, error) {
	return (&URLEncodedForm{data: data}).Reader()
}
