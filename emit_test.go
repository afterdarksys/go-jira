package jira

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEmitFormats(t *testing.T) {
	var issue Issue
	raw := []byte(`{"id":"100","key":"PROJ-1","fields":{"summary":"Ship","customfield_10001":"keep-me","description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Hello"}]}]}}}`)
	if err := json.Unmarshal(raw, &issue); err != nil {
		t.Fatal(err)
	}
	if issue.Fields.Description != "Hello" {
		t.Fatal(issue.Fields.Description)
	}
	for _, format := range []string{"json", "YAML", "yml", "toml"} {
		var buf bytes.Buffer
		if err := Emit(&buf, format, &issue); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		out := buf.String()
		if !strings.Contains(out, "keep-me") || !strings.Contains(out, "PROJ-1") || !strings.Contains(out, "Hello") {
			t.Fatalf("%s: %s", format, out)
		}
	}
}

func TestEmitTOMLArrayAndNil(t *testing.T) {
	var buf bytes.Buffer
	if err := Emit(&buf, "toml", []string{"alpha", "beta"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "items") || !strings.Contains(buf.String(), "alpha") {
		t.Fatal(buf.String())
	}
	buf.Reset()
	if err := Emit(&buf, "toml", map[string]any{"gone": nil, "kept": "yes"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "gone") || !strings.Contains(buf.String(), "kept") {
		t.Fatal(buf.String())
	}
}

func TestEmitRejects(t *testing.T) {
	if err := Emit(nil, "json", map[string]any{"a": 1}); err == nil {
		t.Fatal("nil writer")
	}
	var buf bytes.Buffer
	if err := Emit(&buf, "xml", map[string]any{"a": 1}); err == nil {
		t.Fatal("xml")
	}
	var issue *Issue
	if err := Emit(&buf, "json", issue); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(buf.String()) != "null" {
		t.Fatal(buf.String())
	}
}

func TestEmitResult(t *testing.T) {
	var buf bytes.Buffer
	err := Emit(&buf, "json", Result{OK: true, Action: "assign", Issue: "PROJ-1", AccountID: "acct-1"})
	if err != nil {
		t.Fatal(err)
	}
	var got Result
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Action != "assign" || got.Issue != "PROJ-1" || got.AccountID != "acct-1" {
		t.Fatalf("%+v", got)
	}
}
