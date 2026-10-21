package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticConfigureStatus(t *testing.T) {
	home := t.TempDir()
	env := map[string]string{"HOME": home, "JIRA_PAT": "secret-token"}
	var out, errs bytes.Buffer
	code := run([]string{"configure", "--url", "https://jira.example.com/jira", "--deployment", "datacenter"}, env, &out, &errs, nil)
	if code != 0 {
		t.Fatalf("%d %s", code, &errs)
	}
	path := filepath.Join(home, ".go-jira", "go-jirarc")
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(data), "secret-token") {
		t.Fatal("stored token")
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
	out.Reset()
	code = run([]string{"status"}, env, &out, &errs, nil)
	if code != 0 || !strings.Contains(out.String(), `"tokenPresent": true`) || strings.Contains(out.String(), "secret-token") {
		t.Fatalf("%d %s %s", code, &out, &errs)
	}
	delete(env, "JIRA_PAT")
	out.Reset()
	if code = run([]string{"doctor"}, env, &out, &errs, nil); code != 4 {
		t.Fatalf("%d %s", code, &errs)
	}
}
func TestDiagnosticInvalidArguments(t *testing.T) {
	env := map[string]string{"JIRA_CONFIG": "-", "JIRA_URL": "https://jira.example.com", "JIRA_EMAIL": "a@example.com"}
	for _, args := range [][]string{{"doctor", "extra"}, {"search"}, {"status", "--timeout", "121"}, {"status", "--output", "invalid"}, {"configure"}} {
		var out, errs bytes.Buffer
		if code := run(args, env, &out, &errs, nil); code != 2 {
			t.Fatalf("%v: %d %s", args, code, &errs)
		}
	}
}
func TestDiagnosticConfigurePreservesLegacySettings(t *testing.T) {
	home := t.TempDir()
	env := map[string]string{"HOME": home}
	var out, errs bytes.Buffer
	if code := run([]string{"configure", "--url", "https://example.com", "--email", "a@example.com"}, env, &out, &errs, nil); code != 0 {
		t.Fatal(errs.String())
	}
	cfg, e := loadConfig(env)
	if e != nil || cfg.Email != "a@example.com" || cfg.BaseURL != "https://example.com" {
		t.Fatalf("%+v %v", cfg, e)
	}
}

func TestDiagnosticSwitchDeploymentAndGlobalFlags(t *testing.T) {
	env := map[string]string{"HOME": t.TempDir(), "JIRA_PAT": "secret-pat"}
	var out, errs bytes.Buffer
	for _, args := range [][]string{
		{"configure", "--url", "https://example.com", "--email", "a@example.com", "--cloud-id", "cloud-id"},
		{"configure", "--url", "https://jira.example.com/jira", "--deployment", "datacenter"},
		{"-o", "plain", "status"},
	} {
		out.Reset()
		if code := run(args, env, &out, &errs, nil); code != 0 {
			t.Fatalf("%v: %d %s", args, code, &errs)
		}
	}
	if !strings.Contains(out.String(), `"tokenEnv": "JIRA_PAT"`) || !strings.Contains(out.String(), `"tokenPresent": true`) || strings.Contains(out.String(), "cloud-id") {
		t.Fatal(out.String())
	}
}
