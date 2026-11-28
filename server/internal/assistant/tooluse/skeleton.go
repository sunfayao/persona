package tooluse

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
)

const (
	// skeletonDepth is how deep objects and arrays are drawn; deeper ones are "...".
	skeletonDepth = 4
	// skeletonMax bounds a skeleton, in bytes.
	skeletonMax = 300
	// keyMax is the longest key kept by name.
	keyMax = 40
)

// Skeleton is the shape of a call's arguments with no value in it: a string, number, boolean or null becomes its type
// (str, num, bool, null), an object its keys and their shapes, an array the shapes of its elements (each shape once).
// A key is kept only when it reads like a schema's field name (a lower-case letter, then lower-case letters, digits
// and '_', at most 40 bytes); any other key (an environment variable's name, a header, a path, a user's word) is "*".
// Keys are sorted, objects and arrays nest at most 4 deep, and the result is cut at 300 bytes. Arguments that are not
// JSON are "invalid"; none are "{}".
//
//	{"steps":[{"launch_app":{"name":"Calculator"}}]} → {steps:[{launch_app:{name:str}}]}
func Skeleton(args json.RawMessage) string {
	args = bytes.TrimSpace(args)
	if len(args) == 0 || bytes.Equal(args, []byte("null")) {
		return "{}"
	}
	d := json.NewDecoder(bytes.NewReader(args))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return "invalid"
	}
	var b strings.Builder
	shape(&b, v, 0)
	s := b.String()
	if len(s) > skeletonMax {
		s = s[:skeletonMax-3] + "..."
	}
	return s
}

func shape(b *strings.Builder, v any, depth int) {
	switch v := v.(type) {
	case map[string]any:
		if depth >= skeletonDepth {
			b.WriteString("{...}")
			return
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		b.WriteByte('{')
		first := true
		seen := map[string]bool{}
		for _, k := range keys {
			name := keyName(k)
			// Hidden keys of the same shape are drawn once: "*" stands for any number of them.
			var one strings.Builder
			one.WriteString(name)
			one.WriteByte(':')
			shape(&one, v[k], depth+1)
			if seen[one.String()] {
				continue
			}
			seen[one.String()] = true
			if !first {
				b.WriteByte(',')
			}
			first = false
			b.WriteString(one.String())
		}
		b.WriteByte('}')
	case []any:
		if depth >= skeletonDepth {
			b.WriteString("[...]")
			return
		}
		var shapes []string
		for _, e := range v {
			var one strings.Builder
			shape(&one, e, depth+1)
			if !slices.Contains(shapes, one.String()) {
				shapes = append(shapes, one.String())
			}
		}
		b.WriteByte('[')
		b.WriteString(strings.Join(shapes, "|"))
		b.WriteByte(']')
	case string:
		b.WriteString("str")
	case json.Number:
		b.WriteString("num")
	case bool:
		b.WriteString("bool")
	default:
		b.WriteString("null")
	}
}

// keyName is a key as a skeleton shows it: itself when it reads like a schema's field name, else "*".
func keyName(k string) string {
	if k == "" || len(k) > keyMax || k[0] < 'a' || k[0] > 'z' {
		return "*"
	}
	for i := 1; i < len(k); i++ {
		c := k[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return "*"
		}
	}
	return k
}
