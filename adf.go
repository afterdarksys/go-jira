package jira

import (
	"encoding/json"
	"errors"
	"strings"
)

const (
	maxADFBytes = 1 << 20
	maxADFNodes = 10000
	maxADFDepth = 32
)

func plainADF(text string) map[string]any {
	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": []any{
			map[string]any{
				"type": "paragraph",
				"content": []any{
					map[string]any{"type": "text", "text": text},
				},
			},
		},
	}
}

func adfPlain(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	if len(raw) > maxADFBytes {
		return "", errors.New("jira: description is too large")
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, nil
	}
	var node any
	if err := json.Unmarshal(raw, &node); err != nil {
		return "", errors.New("jira: description is not a document")
	}
	var b strings.Builder
	nodes := 0
	if err := walkADF(&b, node, 0, &nodes); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}

func walkADF(b *strings.Builder, node any, depth int, nodes *int) error {
	if depth > maxADFDepth {
		return errors.New("jira: description is too deep")
	}
	*nodes++
	if *nodes > maxADFNodes {
		return errors.New("jira: description is too large")
	}
	switch t := node.(type) {
	case map[string]any:
		if t["type"] == "text" {
			if s, ok := t["text"].(string); ok {
				b.WriteString(s)
			}
		}
		if content, ok := t["content"].([]any); ok {
			for _, child := range content {
				if err := walkADF(b, child, depth+1, nodes); err != nil {
					return err
				}
			}
		}
		switch t["type"] {
		case "paragraph", "heading", "listItem", "codeBlock":
			b.WriteByte('\n')
		}
	case []any:
		for _, child := range t {
			if err := walkADF(b, child, depth+1, nodes); err != nil {
				return err
			}
		}
	}
	return nil
}
