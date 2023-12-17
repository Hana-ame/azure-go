package myfetch

import (
	"io"
	"net/http"
)

func FetchWithRequest(req *http.Request) (*http.Response, error) {
	return Client().Do(req)
}

func NewRequest(method, url string, header map[string]string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(
		method,
		url,
		body,
	)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	return req, nil
}

// this function make a request and return a response
func Fetch(method, url string, header map[string]string, body io.Reader) (*http.Response, error) {
	req, err := NewRequest(method, url, header, body)
	if err != nil {
		return nil, err
	}
	return FetchWithRequest(req)
}
