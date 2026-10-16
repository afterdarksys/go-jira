package jira

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

// Emit writes v as json, yaml (or yml), or toml.
// Values that carry the original Jira body keep every field from that body.
// A top-level array is wrapped as "items" in TOML, which has no top-level array.
func Emit(w io.Writer, format string, v any) error {
	if w == nil {
		return errors.New("jira: nil writer")
	}
	tree, err := toTree(v)
	if err != nil {
		return err
	}
	switch strings.ToLower(format) {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(tree)
	case "yaml", "yml":
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		if err := enc.Encode(tree); err != nil {
			return err
		}
		return enc.Close()
	case "toml":
		if arr, ok := tree.([]any); ok {
			tree = map[string]any{"items": arr}
		}
		tree, err = dropNil(tree)
		if err != nil {
			return err
		}
		enc := toml.NewEncoder(w)
		return enc.Encode(tree)
	default:
		return fmt.Errorf("jira: unknown format %q", format)
	}
}

type rawJSON interface {
	RawJSON() []byte
}

func rawBytes(v any) []byte {
	if v == nil || !callable(v) {
		return nil
	}
	r, ok := v.(rawJSON)
	if !ok {
		return nil
	}
	return r.RawJSON()
}

func callable(v any) bool {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return !rv.IsNil()
	default:
		return true
	}
}

func toTree(v any) (any, error) {
	raw := rawBytes(v)
	if len(bytes.TrimSpace(raw)) == 0 {
		var err error
		raw, err = json.Marshal(v)
		if err != nil {
			return nil, err
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, err
	}
	return normalize(tree)
}

func normalize(v any) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			nv, err := normalize(val)
			if err != nil {
				return nil, err
			}
			out[k] = nv
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			nv, err := normalize(val)
			if err != nil {
				return nil, err
			}
			out[i] = nv
		}
		return out, nil
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i, nil
		}
		f, err := t.Float64()
		if err != nil {
			return nil, err
		}
		return f, nil
	default:
		return v, nil
	}
}

func dropNil(v any) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if val == nil {
				continue
			}
			nv, err := dropNil(val)
			if err != nil {
				return nil, err
			}
			out[k] = nv
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			nv, err := dropNil(val)
			if err != nil {
				return nil, err
			}
			out[i] = nv
		}
		return out, nil
	default:
		return v, nil
	}
}
