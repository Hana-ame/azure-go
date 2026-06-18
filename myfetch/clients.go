package myfetch

import (
	"net"
	"net/http"
	"time"
)

type clients struct {
	cnt     uint8
	clients []*http.Client
}

func (cs *clients) pick() *http.Client {
	cs.cnt++
	if len(cs.clients) == 0 {
		return http.DefaultClient
	} else {
		return cs.clients[len(cs.clients)%int(cs.cnt)]
	}
}

func (cs *clients) setClients(clients []*http.Client) {
	cs.clients = clients
}

var DefaultClients *clients = &clients{cnt: 0, clients: []*http.Client{}}
var DefaultClient = &http.Client{
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
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

func SetClients(clients []*http.Client) {
	DefaultClients.setClients(clients)
}
