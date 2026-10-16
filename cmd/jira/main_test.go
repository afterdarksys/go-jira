package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/afterdarksys/go-jira"
)

func TestCLIUsageDoesNotDial(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code int
		out  bool
		sub  string
	}{
		{name: "no args", args: nil, code: 2, sub: "usage:"},
		{name: "help", args: []string{"help"}, code: 0, out: true, sub: "view ISSUE"},
		{name: "dash h", args: []string{"-h"}, code: 0, sub: "usage:"},
		{name: "unknown", args: []string{"nope"}, code: 2, sub: "unknown command"},
		{name: "view help", args: []string{"view", "-h"}, code: 0, sub: "usage:"},
		{name: "bad format", args: []string{"-o", "xml", "view", "PROJ-1"}, code: 2, sub: "unknown format"},
		{name: "token flag", args: []string{"-token", "nope", "view", "PROJ-1"}, code: 2, sub: "flag provided but not defined"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runNoNet(t, tc.args)
			if code != tc.code {
				t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
			}
			text := stderr
			if tc.out {
				text = stdout
			}
			if !strings.Contains(text, tc.sub) {
				t.Fatalf("missing %q in %s", tc.sub, text)
			}
			if strings.Contains(stdout+stderr, "JIRA_API_TOKEN is not set") && tc.name == "unknown" {
				t.Fatal(stderr)
			}
		})
	}
}

func TestCLIRequiresToken(t *testing.T) {
	called := false
	var stdout, stderr bytes.Buffer
	code := run([]string{"view", "PROJ-1"}, map[string]string{
		"JIRA_BASE_URL": "https://example.com",
		"JIRA_EMAIL":    "user@example.com",
		"JIRA_CONFIG":   "-",
	}, &stdout, &stderr, func(string, string, string) (*jira.Client, error) {
		called = true
		t.Fatal("dialed")
		return nil, nil
	})
	if called {
		t.Fatal("dialed")
	}
	if code != 2 || !strings.Contains(stderr.String(), "JIRA_API_TOKEN is not set") {
		t.Fatalf("%d %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatal(stdout.String())
	}
}

func TestCLICommands(t *testing.T) {
	var hit struct {
		method string
		path   string
		query  string
		body   string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		hit.method = r.Method
		hit.path = r.URL.Path
		hit.query = r.URL.RawQuery
		hit.body = string(b)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/watchers"):
			_, _ = io.WriteString(w, `{"watchCount":0,"isWatching":false,"watchers":[]}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/worklog"):
			_, _ = io.WriteString(w, `{"startAt":0,"maxResults":100,"total":0,"worklogs":[]}`)
		case r.URL.Path == "/rest/api/3/group/member":
			_, _ = io.WriteString(w, `{"total":0,"isLast":true,"values":[]}`)
		case r.URL.Path == "/rest/api/3/group/user" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"name":"devs","groupId":"group-1"}`)
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"id":"1","key":"PROJ-1","fields":{"summary":"Ship","customfield_10001":"keep-me"}}`)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/worklog"):
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"9","timeSpent":"2h","timeSpentSeconds":7200}`)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)

	call := func(t *testing.T, args []string, format string) (int, string, string) {
		t.Helper()
		env := map[string]string{
			"JIRA_BASE_URL":  srv.URL,
			"JIRA_EMAIL":     "user@example.com",
			"JIRA_API_TOKEN": "token-value-1",
			"JIRA_CONFIG":    "-",
		}
		if format != "" {
			env["JIRA_OUTPUT"] = format
		}
		var stdout, stderr bytes.Buffer
		code := run(args, env, &stdout, &stderr, func(base, email, token string) (*jira.Client, error) {
			return jira.New(base, email, token, jira.WithLoopbackHTTP())
		})
		if strings.Contains(stdout.String()+stderr.String(), "token-value-1") {
			t.Fatalf("token leaked\n%s\n%s", stdout.String(), stderr.String())
		}
		return code, stdout.String(), stderr.String()
	}

	t.Run("view json", func(t *testing.T) {
		code, stdout, stderr := call(t, []string{"view", "PROJ-1"}, "")
		if code != 0 {
			t.Fatal(stderr)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(stdout), &m); err != nil {
			t.Fatal(err, stdout)
		}
		if m["key"] != "PROJ-1" || !strings.Contains(stdout, "keep-me") {
			t.Fatal(stdout)
		}
	})
	t.Run("view yaml", func(t *testing.T) {
		code, stdout, stderr := call(t, []string{"-o", "yaml", "view", "PROJ-1"}, "")
		if code != 0 || !strings.Contains(stdout, "keep-me") || !strings.Contains(stdout, "PROJ-1") {
			t.Fatalf("%d %s %s", code, stdout, stderr)
		}
	})
	t.Run("view toml", func(t *testing.T) {
		code, stdout, stderr := call(t, []string{"view", "PROJ-1"}, "toml")
		if code != 0 || !strings.Contains(stdout, "keep-me") {
			t.Fatalf("%d %s %s", code, stdout, stderr)
		}
	})
	t.Run("update", func(t *testing.T) {
		code, stdout, stderr := call(t, []string{"update", "PROJ-1", "-summary", "Renamed", "-labels", "a,b", "-due", "2026-10-16", "-time-spent", "1h"}, "")
		if code != 0 || !strings.Contains(stdout, `"action": "update"`) {
			t.Fatalf("%d %s %s", code, stdout, stderr)
		}
		if hit.method != http.MethodPut || hit.path != "/rest/api/3/issue/PROJ-1" {
			t.Fatalf("%s %s", hit.method, hit.path)
		}
		if !strings.Contains(hit.body, `"summary":"Renamed"`) || !strings.Contains(hit.body, `"duedate":"2026-10-16"`) {
			t.Fatal(hit.body)
		}
		if !strings.Contains(hit.body, `"labels":["a","b"]`) || !strings.Contains(hit.body, `"timeSpent":"1h"`) {
			t.Fatal(hit.body)
		}
		code, stdout, stderr = call(t, []string{"update", "-clear-due", "PROJ-1"}, "")
		if code != 0 || !strings.Contains(hit.body, `"duedate":null`) {
			t.Fatalf("%d %s %s %s", code, stdout, stderr, hit.body)
		}
	})
	t.Run("assign", func(t *testing.T) {
		code, stdout, stderr := call(t, []string{"assign", "PROJ-1", "-account", "acct-1"}, "")
		if code != 0 || hit.path != "/rest/api/3/issue/PROJ-1/assignee" || !strings.Contains(hit.body, `"accountId":"acct-1"`) {
			t.Fatalf("%d %s %s %s", code, stdout, stderr, hit.body)
		}
	})
	t.Run("unassign", func(t *testing.T) {
		code, _, stderr := call(t, []string{"unassign", "PROJ-1"}, "")
		if code != 0 || !strings.Contains(hit.body, `"accountId":null`) {
			t.Fatalf("%d %s %s", code, stderr, hit.body)
		}
	})
	t.Run("watch", func(t *testing.T) {
		code, _, stderr := call(t, []string{"watch", "add", "PROJ-1", "-account", "acct-1"}, "")
		if code != 0 || hit.method != http.MethodPost || hit.body != `"acct-1"` {
			t.Fatalf("%d %s %s %s", code, stderr, hit.method, hit.body)
		}
		code, _, stderr = call(t, []string{"watch", "remove", "PROJ-1", "-account", "acct-1"}, "")
		if code != 0 || hit.method != http.MethodDelete || !strings.Contains(hit.query, "accountId=acct-1") {
			t.Fatalf("%d %s %s", code, stderr, hit.query)
		}
		code, stdout, stderr := call(t, []string{"watch", "list", "PROJ-1"}, "")
		if code != 0 || !strings.Contains(stdout, "watchCount") {
			t.Fatalf("%d %s %s", code, stdout, stderr)
		}
	})
	t.Run("time", func(t *testing.T) {
		code, stdout, stderr := call(t, []string{"time", "add", "PROJ-1", "-spent", "2h", "-adjust", "leave"}, "")
		if code != 0 || hit.method != http.MethodPost || !strings.Contains(hit.query, "adjustEstimate=leave") {
			t.Fatalf("%d %s %s %s", code, stdout, stderr, hit.query)
		}
		if !strings.Contains(hit.body, `"timeSpent":"2h"`) {
			t.Fatal(hit.body)
		}
	})
	t.Run("group", func(t *testing.T) {
		code, stdout, stderr := call(t, []string{"group", "add", "-group", "group-1", "-account", "acct-1"}, "")
		if code != 0 || hit.method != http.MethodPost || !strings.Contains(stdout, "devs") {
			t.Fatalf("%d %s %s", code, stdout, stderr)
		}
		code, stdout, stderr = call(t, []string{"group", "members", "-group", "group-1"}, "")
		if code != 0 || !strings.Contains(hit.query, "maxResults=50") {
			t.Fatalf("%d %s %s %s", code, stdout, stderr, hit.query)
		}
		code, _, stderr = call(t, []string{"group", "remove", "-group", "group-1", "-account", "acct-1"}, "")
		if code != 0 || hit.method != http.MethodDelete {
			t.Fatalf("%d %s", code, stderr)
		}
	})
	t.Run("assign usage", func(t *testing.T) {
		code, _, stderr := call(t, []string{"assign", "PROJ-1"}, "")
		if code != 2 || !strings.Contains(stderr, "-account") {
			t.Fatalf("%d %s", code, stderr)
		}
	})
}

func TestConfigFile(t *testing.T) {
	const secret = "file-token-1"
	write := func(t *testing.T, body string, mode os.FileMode) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "go-jirarc")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	body := func(token string) string {
		return "base_url = \"https://file.example\"\nemail = \"file@example.com\"\noutput = \"yaml\"\napi_token = \"" + token + "\"\n"
	}
	assertHidden := func(t *testing.T, text string) {
		t.Helper()
		if strings.Contains(text, secret) {
			t.Fatal("token leaked")
		}
	}

	t.Run("default path", func(t *testing.T) {
		home := t.TempDir()
		dir := filepath.Join(home, ".go-jira")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "go-jirarc")
		if err := os.WriteFile(path, []byte(body(secret)), 0o600); err != nil {
			t.Fatal(err)
		}
		var gotBase, gotEmail, gotToken string
		var stdout, stderr bytes.Buffer
		code := run([]string{"view", "PROJ-1"}, map[string]string{"HOME": home}, &stdout, &stderr, func(base, email, token string) (*jira.Client, error) {
			gotBase, gotEmail, gotToken = base, email, token
			return nil, io.EOF
		})
		assertHidden(t, stdout.String()+stderr.String())
		if code != 1 || gotBase != "https://file.example" || gotEmail != "file@example.com" || gotToken != secret {
			t.Fatalf("%d %s %s %s", code, stderr.String(), gotBase, gotEmail)
		}
	})

	t.Run("flag and env override file", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"key":"PROJ-1","fields":{"summary":"Ship"}}`)
		}))
		t.Cleanup(srv.Close)
		path := write(t, body(secret), 0o600)
		var gotToken string
		var stdout, stderr bytes.Buffer
		code := run([]string{"-base-url", srv.URL, "-email", "flag@example.com", "view", "PROJ-1"}, map[string]string{
			"JIRA_CONFIG":    path,
			"JIRA_API_TOKEN": "env-token-11",
			"JIRA_OUTPUT":    "json",
			"HOME":           t.TempDir(),
		}, &stdout, &stderr, func(base, email, token string) (*jira.Client, error) {
			gotToken = token
			if base != srv.URL || email != "flag@example.com" {
				t.Fatalf("base %s email %s", base, email)
			}
			return jira.New(base, email, token, jira.WithLoopbackHTTP())
		})
		text := stdout.String() + stderr.String()
		assertHidden(t, text)
		if strings.Contains(text, "env-token-11") {
			t.Fatal("env token leaked")
		}
		if code != 0 || !strings.Contains(stdout.String(), "PROJ-1") || gotToken != "env-token-11" {
			t.Fatalf("%d %s %s", code, stdout.String(), stderr.String())
		}
		if strings.Contains(stdout.String(), "summary:") {
			t.Fatal("file output format overrode the default json")
		}
	})

	t.Run("file format", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"key":"PROJ-1"}`)
		}))
		t.Cleanup(srv.Close)
		path := write(t, "base_url = \""+srv.URL+"\"\nemail = \"file@example.com\"\noutput = \"yaml\"\napi_token = \""+secret+"\"\n", 0o600)
		var stdout, stderr bytes.Buffer
		code := run([]string{"view", "PROJ-1"}, map[string]string{"JIRA_CONFIG": path}, &stdout, &stderr, func(base, email, token string) (*jira.Client, error) {
			return jira.New(base, email, token, jira.WithLoopbackHTTP())
		})
		assertHidden(t, stdout.String()+stderr.String())
		if code != 0 || !strings.Contains(stdout.String(), "key:") {
			t.Fatalf("%d %s %s", code, stdout.String(), stderr.String())
		}
	})

	t.Run("skip file", func(t *testing.T) {
		home := t.TempDir()
		dir := filepath.Join(home, ".go-jira")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go-jirarc"), []byte(body(secret)), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(dir, "go-jirarc"), 0o666); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o777); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		code := run([]string{"view", "PROJ-1"}, map[string]string{
			"HOME":        home,
			"JIRA_CONFIG": "-",
		}, &stdout, &stderr, func(string, string, string) (*jira.Client, error) {
			t.Fatal("dialed")
			return nil, nil
		})
		assertHidden(t, stdout.String()+stderr.String())
		if code != 2 || !strings.Contains(stderr.String(), "JIRA_BASE_URL") {
			t.Fatalf("%d %s", code, stderr.String())
		}
	})

	t.Run("rejects loose file", func(t *testing.T) {
		path := write(t, body(secret), 0o644)
		var stdout, stderr bytes.Buffer
		called := false
		code := run([]string{"view", "PROJ-1"}, map[string]string{"JIRA_CONFIG": path}, &stdout, &stderr, func(string, string, string) (*jira.Client, error) {
			called = true
			return nil, nil
		})
		assertHidden(t, stdout.String()+stderr.String())
		if called || code != 2 || !strings.Contains(stderr.String(), "readable only by its owner") {
			t.Fatalf("%d %s", code, stderr.String())
		}
	})

	t.Run("rejects loose directory", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "go-jirarc")
		if err := os.WriteFile(path, []byte(body(secret)), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o707); err != nil {
			t.Fatal(err)
		}
		var stderr bytes.Buffer
		code := run([]string{"view", "PROJ-1"}, map[string]string{"JIRA_CONFIG": path}, io.Discard, &stderr, func(string, string, string) (*jira.Client, error) {
			t.Fatal("dialed")
			return nil, nil
		})
		assertHidden(t, stderr.String())
		if code != 2 || !strings.Contains(stderr.String(), "directory must not be writable") {
			t.Fatalf("%d %s", code, stderr.String())
		}
	})

	t.Run("rejects symlink", func(t *testing.T) {
		path := write(t, body(secret), 0o600)
		link := filepath.Join(t.TempDir(), "go-jirarc")
		if err := os.Symlink(path, link); err != nil {
			t.Fatal(err)
		}
		var stderr bytes.Buffer
		code := run([]string{"view", "PROJ-1"}, map[string]string{"JIRA_CONFIG": link}, io.Discard, &stderr, func(string, string, string) (*jira.Client, error) {
			t.Fatal("dialed")
			return nil, nil
		})
		assertHidden(t, stderr.String())
		if code != 2 || !strings.Contains(stderr.String(), "must not be a symlink") {
			t.Fatalf("%d %s", code, stderr.String())
		}
	})

	t.Run("rejects oversized", func(t *testing.T) {
		path := write(t, strings.Repeat("a", maxConfigSize+1), 0o600)
		var stderr bytes.Buffer
		code := run([]string{"view", "PROJ-1"}, map[string]string{"JIRA_CONFIG": path}, io.Discard, &stderr, func(string, string, string) (*jira.Client, error) {
			t.Fatal("dialed")
			return nil, nil
		})
		if code != 2 || !strings.Contains(stderr.String(), "too large") {
			t.Fatalf("%d %s", code, stderr.String())
		}
	})

	t.Run("invalid file hides token", func(t *testing.T) {
		path := write(t, "api_token = \""+secret+"\noutput = \"nope\"\n", 0o600)
		var stderr bytes.Buffer
		code := run([]string{"view", "PROJ-1"}, map[string]string{"JIRA_CONFIG": path}, io.Discard, &stderr, func(string, string, string) (*jira.Client, error) {
			t.Fatal("dialed")
			return nil, nil
		})
		assertHidden(t, stderr.String())
		if code != 2 || stderr.String() != "invalid go-jirarc\n" {
			t.Fatalf("%d %q", code, stderr.String())
		}
	})

	t.Run("unknown format from file", func(t *testing.T) {
		path := write(t, "base_url = \"https://example.com\"\nemail = \"file@example.com\"\noutput = \"xml\"\napi_token = \""+secret+"\"\n", 0o600)
		var stderr bytes.Buffer
		code := run([]string{"view", "PROJ-1"}, map[string]string{"JIRA_CONFIG": path}, io.Discard, &stderr, func(string, string, string) (*jira.Client, error) {
			t.Fatal("dialed")
			return nil, nil
		})
		assertHidden(t, stderr.String())
		if code != 2 || !strings.Contains(stderr.String(), "unknown format") {
			t.Fatalf("%d %s", code, stderr.String())
		}
	})

	t.Run("explicit missing path", func(t *testing.T) {
		var stderr bytes.Buffer
		code := run([]string{"view", "PROJ-1"}, map[string]string{
			"JIRA_CONFIG": filepath.Join(t.TempDir(), "missing"),
		}, io.Discard, &stderr, func(string, string, string) (*jira.Client, error) {
			t.Fatal("dialed")
			return nil, nil
		})
		if code != 2 || !strings.Contains(stderr.String(), "not found") {
			t.Fatalf("%d %s", code, stderr.String())
		}
	})
}

func runNoNet(t *testing.T, args []string) (int, string, string) {
	t.Helper()
	called := false
	var stdout, stderr bytes.Buffer
	code := run(args, map[string]string{"JIRA_CONFIG": "-"}, &stdout, &stderr, func(string, string, string) (*jira.Client, error) {
		called = true
		t.Fatal("dialed")
		return nil, nil
	})
	if called {
		t.Fatal("dialed")
	}
	return code, stdout.String(), stderr.String()
}
