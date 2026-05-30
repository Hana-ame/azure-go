package orderedmap

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestOrderedMap_Basic(t *testing.T) {
	o := New()
	o.Set("number", 4)
	o.Set("string", "x")

	if v, ok := o.Get("number"); !ok || v.(int) != 4 {
		t.Error("Get number failed")
	}
	if v, ok := o.Get("string"); !ok || v.(string) != "x" {
		t.Error("Get string failed")
	}
	if v := o.GetOrDefault("missing", "default"); v.(string) != "default" {
		t.Error("GetOrDefault failed")
	}
	if l := o.Len(); l != 2 {
		t.Error("Len failed:", l)
	}
}

func TestOrderedMap_Keys(t *testing.T) {
	o := New()
	o.Set("b", 1)
	o.Set("a", 2)

	keys := o.Keys()
	if keys[0] != "b" || keys[1] != "a" {
		t.Error("Keys order wrong:", keys)
	}
}

func TestOrderedMap_Delete(t *testing.T) {
	o := New()
	o.Set("a", 1)
	o.Set("b", 2)
	o.Delete("a")
	if o.Len() != 1 {
		t.Error("Delete failed: len", o.Len())
	}
	if _, ok := o.Get("a"); ok {
		t.Error("Delete failed: still has a")
	}
}

func TestOrderedMap_Values(t *testing.T) {
	o := New()
	o.Set("number", 4)
	o.Set("string", "x")

	values := o.Values()
	expected := map[string]interface{}{
		"number": 4,
		"string": "x",
	}
	if !reflect.DeepEqual(values, expected) {
		t.Error("Values failed:", values)
	}
}

func TestOrderedMap_MarshalJSON(t *testing.T) {
	o := New()
	o.Set("a", 1)
	o.Set("b", 2)
	b, err := o.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); s != `{"a":1,"b":2}` {
		t.Error("MarshalJSON failed:", s)
	}
}

func TestOrderedMap_UnmarshalJSON(t *testing.T) {
	s := `{"x":"<>","y":2}`
	o := New()
	if err := json.Unmarshal([]byte(s), &o); err != nil {
		t.Fatal(err)
	}
	if v, ok := o.Get("x"); !ok || v.(string) != "<>" {
		t.Error("UnmarshalJSON x failed:", v)
	}
	// json.Unmarshal decodes numbers as float64
	if v, ok := o.Get("y"); !ok || v.(float64) != 2 {
		t.Error("UnmarshalJSON y failed:", v)
	}
}

func TestOrderedMap_SetEscapeHTML(t *testing.T) {
	o := New()
	o.SetEscapeHTML(false)
	o.Set("specialstring", "<>&")
	b, err := o.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"specialstring":"<>&"}` {
		t.Error("SetEscapeHTML Marshal failed:", string(b))
	}
}

func TestOrderedMap_SortKeys(t *testing.T) {
	o := New()
	o.Set("b", 2)
	o.Set("a", 1)
	o.Set("c", 3)
	o.SortKeys(sortStrings)
	expected := []string{"a", "b", "c"}
	for i, k := range o.Keys() {
		if k.(string) != expected[i] {
			t.Error("SortKeys order wrong at", i, k)
		}
	}
}

func sortStrings(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func TestOrderedMap_Sort(t *testing.T) {
	o := New()
	o.Set("a", 1.0)
	o.Set("b", 2.0)
	o.Set("c", 3.0)
	o.Sort(func(a *Pair, b *Pair) bool {
		return a.value.(float64) > b.value.(float64)
	})
	expectedKeys := []string{"c", "b", "a"}
	for i, k := range o.Keys() {
		if k.(string) != expectedKeys[i] {
			t.Error("Sort order wrong at", i, k)
		}
	}
}
