package myiter

import "github.com/Hana-ame/azure-go/Tools/orderedmap"

type Iter struct {
	m *orderedmap.OrderedMap
}

func NewIter(m *orderedmap.OrderedMap) *Iter {
	return &Iter{m: m}
}

func (it *Iter) Iter(fn func(k, v any) bool) {
	if it == nil || it.m == nil {
		return
	}
	for _, k := range it.m.Keys() {
		v, _ := it.m.Get(k)
		if fn(k, v) {
			break
		}
	}
}
