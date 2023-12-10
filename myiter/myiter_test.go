package myiter

import (
	"fmt"
	"testing"

	"github.com/Hana-ame/orderedmap"
)

func TestX(t *testing.T) {
	o := orderedmap.New()
	o.Set("a", "11")
	o.Set("b", "22")
	o.Set("c", "33")
	iter := &OrderedMapIter{o}
	i := "00"
	f := func(k, v any) bool {
		fmt.Println(k, v)
		i += v.(string)
		return false
	}
	iter.Iter(f)
	fmt.Println(i)
}
