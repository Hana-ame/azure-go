package myiter

import "github.com/iancoleman/orderedmap"

type SliceIter []any

func (i SliceIter) Iter(iterator Iterator) {
	for k, v := range i {
		isStopping := iterator.Iterate(k, v)
		if isStopping {
			return
		}
	}
}

type MapIter map[any]any

func (i MapIter) Iter(iterator Iterator) {
	for k, v := range i {
		isStopping := iterator.Iterate(k, v)
		if isStopping {
			return
		}
	}
}

type OrderedMapIter struct {
	*orderedmap.OrderedMap
}

func (i OrderedMapIter) Iter(iterator Iterator) {
	for _, k := range i.Keys() {
		v, isExist := i.Get(k)
		if isExist {
			isStopping := iterator.Iterate(k, v)
			if isStopping {
				return
			}
		}
	}
}

type Iterator interface {
	Iterate(k, v any) bool
}

type Iterable interface {
	Iter()
}
