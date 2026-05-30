package orderedmap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

type Pair struct {
	key   any
	value any
}

type OrderedMap struct {
	keys          []any
	values        []any
	m             map[any]int
	escapeHTML    bool
	escapeHTMLSet bool
}

func New() *OrderedMap {
	return &OrderedMap{
		m:          make(map[any]int),
		escapeHTML: true,
	}
}

func (o *OrderedMap) Set(key, value any) {
	if idx, ok := o.m[key]; ok {
		o.values[idx] = value
		return
	}
	o.m[key] = len(o.keys)
	o.keys = append(o.keys, key)
	o.values = append(o.values, value)
}

func (o *OrderedMap) Get(key any) (any, bool) {
	if idx, ok := o.m[key]; ok {
		return o.values[idx], true
	}
	return nil, false
}

func (o *OrderedMap) GetOrDefault(key any, defaultVal any) any {
	if v, ok := o.Get(key); ok {
		return v
	}
	return defaultVal
}

func (o *OrderedMap) Delete(key any) {
	if idx, ok := o.m[key]; ok {
		o.keys = append(o.keys[:idx], o.keys[idx+1:]...)
		o.values = append(o.values[:idx], o.values[idx+1:]...)
		delete(o.m, key)
		// update indices after the deleted one
		for i := idx; i < len(o.keys); i++ {
			o.m[o.keys[i]] = i
		}
	}
}

func (o *OrderedMap) Keys() []any {
	return o.keys
}

func (o *OrderedMap) Values() map[string]interface{} {
	result := make(map[string]interface{})
	for i, k := range o.keys {
		switch v := o.values[i].(type) {
		case *OrderedMap:
			result[fmt.Sprintf("%v", k)] = v.Values()
		default:
			result[fmt.Sprintf("%v", k)] = v
		}
	}
	return result
}

func (o *OrderedMap) Len() int {
	return len(o.keys)
}

func (o *OrderedMap) SetEscapeHTML(on bool) {
	o.escapeHTML = on
	o.escapeHTMLSet = true
}

func (o *OrderedMap) SortKeys(sorter func([]string)) {
	strKeys := make([]string, len(o.keys))
	origIndices := make([]int, len(o.keys))
	for i, k := range o.keys {
		strKeys[i] = fmt.Sprintf("%v", k)
		origIndices[i] = i
	}
	sorter(strKeys)
	newKeys := make([]any, len(o.keys))
	newValues := make([]any, len(o.values))
	newM := make(map[any]int)
	for newIdx, s := range strKeys {
		// find original index by matching string key
		for j, k := range o.keys {
			if fmt.Sprintf("%v", k) == s {
				newKeys[newIdx] = o.keys[j]
				newValues[newIdx] = o.values[j]
				newM[o.keys[j]] = newIdx
				break
			}
		}
	}
	o.keys = newKeys
	o.values = newValues
	o.m = newM
}

func (o *OrderedMap) Sort(less func(a *Pair, b *Pair) bool) {
	pairs := make([]*Pair, len(o.keys))
	for i := range o.keys {
		pairs[i] = &Pair{key: o.keys[i], value: o.values[i]}
	}
	sort.Slice(pairs, func(i, j int) bool {
		return less(pairs[i], pairs[j])
	})
	newKeys := make([]any, len(pairs))
	newValues := make([]any, len(pairs))
	newM := make(map[any]int)
	for i, p := range pairs {
		newKeys[i] = p.key
		newValues[i] = p.value
		newM[p.key] = i
	}
	o.keys = newKeys
	o.values = newValues
	o.m = newM
}

func (o *OrderedMap) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("null"), nil
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := o.marshalKey(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		useEscapeHTML := o.escapeHTML
		if !useEscapeHTML && !o.escapeHTMLSet {
			useEscapeHTML = true
		}
		if useEscapeHTML {
			vb, err := json.Marshal(o.values[i])
			if err != nil {
				return nil, err
			}
			buf.Write(vb)
		} else {
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(o.values[i]); err != nil {
				return nil, err
			}
			// remove trailing newline from Encode
			// Strip last byte (newline)
			buf.Truncate(buf.Len() - 1)
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func (o *OrderedMap) marshalKey(k any) ([]byte, error) {
	useEscapeHTML := o.escapeHTML
	if useEscapeHTML {
		return json.Marshal(k)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(k); err != nil {
		return nil, err
	}
	// strip trailing newline
	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	return b, nil
}

func (o *OrderedMap) UnmarshalJSON(data []byte) error {
	o.m = make(map[any]int)
	o.keys = nil
	o.values = nil

	decoder := json.NewDecoder(bytes.NewReader(data))

	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("expected {")
	}

	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		key := fmt.Sprintf("%v", keyToken)
		var value any
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		// Convert nested maps to OrderedMap
		value = o.convertValue(value)
		o.Set(key, value)
	}
	return nil
}

func (o *OrderedMap) convertValue(v any) any {
	switch vv := v.(type) {
	case map[string]interface{}:
		om := *New()
		om.escapeHTML = o.escapeHTML
		om.escapeHTMLSet = o.escapeHTMLSet
		for k, val := range vv {
			om.Set(k, om.convertValue(val))
		}
		return om
	case []interface{}:
		for i, elem := range vv {
			vv[i] = o.convertValue(elem)
		}
		return vv
	default:
		return v
	}
}

func (o *OrderedMap) String() string {
	b, _ := json.Marshal(o)
	return string(b)
}
