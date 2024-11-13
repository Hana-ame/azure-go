package myfetch

import (
	"encoding/json"
	"net/http"

	"github.com/Hana-ame/azure-go/Tools/orderedmap"
)

// this function receive json request.
func ResponseToJson(r *http.Response) (*orderedmap.OrderedMap, error) {
	o := orderedmap.New()
	err := json.NewDecoder(r.Body).Decode(&o)
	return o, err
}
