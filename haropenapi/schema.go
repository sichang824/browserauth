package haropenapi

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
)

type obj = map[string]any

var fieldName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\[\]-]{0,79}$`)
var dynamicID = regexp.MustCompile(`^(?:[0-9]+|[0-9a-fA-F]{16,}|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)

func safeField(s string, secrets map[string]bool) bool {
	if !fieldName.MatchString(s) || dynamicID.MatchString(s) {
		return false
	}
	for v := range secrets {
		if strings.Contains(s, v) {
			return false
		}
	}
	return true
}

func infer(v any, depth int, secrets map[string]bool) obj {
	if depth > 10 {
		return obj{}
	}
	switch x := v.(type) {
	case nil:
		return obj{"type": "null"}
	case bool:
		return obj{"type": "boolean"}
	case json.Number:
		if !strings.ContainsAny(string(x), ".eE") {
			return obj{"type": "integer"}
		}
		return obj{"type": "number"}
	case string:
		return obj{"type": "string"}
	case []any:
		var s obj
		for _, v := range x {
			s = merge(s, infer(v, depth+1, secrets))
		}
		if s == nil {
			s = obj{}
		}
		return obj{"type": "array", "items": s}
	case map[string]any:
		ps := obj{}
		for k, v := range x {
			// Unknown/dynamic object keys may themselves contain private data.
			if !safeField(k, secrets) {
				return obj{"type": "object", "additionalProperties": true}
			}
			ps[k] = infer(v, depth+1, secrets)
		}
		return obj{"type": "object", "properties": ps, "additionalProperties": true}
	}
	return obj{}
}

// No required/enum/default/example is inferred from samples. Empty schemas
// deliberately widen unknown or unavailable bodies rather than asserting a type.
func merge(a, b obj) obj {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if reflect.DeepEqual(a, b) {
		return a
	}
	if len(a) == 0 || len(b) == 0 {
		return obj{}
	}
	if a["type"] == "object" && b["type"] == "object" {
		ap, aok := a["properties"].(obj)
		bp, bok := b["properties"].(obj)
		if !aok || !bok {
			return obj{"type": "object", "additionalProperties": true}
		}
		ps := obj{}
		for k, v := range ap {
			ps[k] = v
		}
		for k, v := range bp {
			old, _ := ps[k].(obj)
			ps[k] = merge(old, v.(obj))
		}
		return obj{"type": "object", "properties": ps, "additionalProperties": true}
	}
	if a["type"] == "array" && b["type"] == "array" {
		return obj{"type": "array", "items": merge(a["items"].(obj), b["items"].(obj))}
	}
	variants := []any{}
	for _, s := range []obj{a, b} {
		vs, ok := s["anyOf"].([]any)
		if !ok {
			vs = []any{s}
		}
		for _, v := range vs {
			found := false
			for i, old := range variants {
				if reflect.DeepEqual(old, v) {
					found = true
					break
				}
				if old.(obj)["type"] != nil && old.(obj)["type"] == v.(obj)["type"] {
					variants[i] = merge(old.(obj), v.(obj))
					found = true
					break
				}
			}
			if !found {
				variants = append(variants, v)
			}
		}
	}
	if len(variants) == 1 {
		return variants[0].(obj)
	}
	return obj{"anyOf": variants}
}
