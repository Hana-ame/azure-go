package myfetch

import (
	"net"
	"net/http"
	"time"
)

var DefaultClient = &http.Client{
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			LocalAddr: &net.TCPAddr{IP: net.IPv4(142, 171, 157, 74)},
			Timeout:   90 * time.Second,
			KeepAlive: 90 * time.Second,
		}).DialContext,
		MaxIdleConns:        100,
		IdleConnTimeout:     10 * time.Second,
		TLSHandshakeTimeout: 30 * time.Second,
	},
	Timeout: 300 * time.Second,
}

// public methods

func Client() *http.Client {
	return DefaultClient
}
