package myfetch

import "net/http"

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

// public methods

func Client() *http.Client {
	return DefaultClients.pick()
}

func SetClients(clients []*http.Client) {
	DefaultClients.setClients(clients)
}
