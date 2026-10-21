package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	jira "github.com/afterdarksys/go-jira"
	"github.com/pelletier/go-toml/v2"
)

func diagnosticCommand(s string) bool {
	switch s {
	case "configure", "status", "doctor", "permissions", "project", "issue", "transitions", "fields", "search", "request":
		return true
	}
	return false
}

type queryFlags []string

func (q *queryFlags) String() string     { return "KEY=VALUE" }
func (q *queryFlags) Set(s string) error { *q = append(*q, s); return nil }
func runDiagnostics(args []string, env map[string]string, out, errOut io.Writer) int {
	cmd := args[0]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(errOut)
	base := fs.String("url", "", "Jira HTTPS base URL")
	fs.StringVar(base, "base-url", "", "Jira HTTPS base URL")
	deployment := fs.String("deployment", "", "cloud or datacenter")
	email := fs.String("email", "", "Cloud account email")
	cloudID := fs.String("cloud-id", "", "scoped-token Cloud ID")
	tokenEnv := fs.String("token-env", "", "token environment variable")
	format := fs.String("output", "", "json, yaml, toml, plain")
	fs.StringVar(format, "o", "", "output format")
	timeout := fs.Int("timeout", 20, "request timeout seconds (1–120)")
	project := fs.String("project", "", "project key")
	issue := fs.String("issue", "", "issue key")
	permissions := fs.String("permissions", jira.DefaultPermissions, "permission keys")
	limit := fs.Int("limit", 50, "maximum issues (1–1000)")
	var params queryFlags
	fs.Var(&params, "param", "query KEY=VALUE (repeatable)")
	if e := fs.Parse(reorderFlags(fs, args[1:])); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(e error) int { fmt.Fprintln(errOut, e); return 2 }
	// A first configure may create an explicit missing config path.
	loadEnv := env
	if cmd == "configure" && env["JIRA_CONFIG"] != "" && env["JIRA_CONFIG"] != "-" {
		if _, e := os.Lstat(env["JIRA_CONFIG"]); errors.Is(e, os.ErrNotExist) {
			loadEnv = map[string]string{"JIRA_CONFIG": "-"}
		}
	}
	cfg, e := loadConfig(loadEnv)
	if e != nil {
		return fail(e)
	}
	seen := visited(fs)
	choose := func(flagValue, envKey, fileValue string, explicit bool) string {
		if explicit {
			return flagValue
		}
		if v, ok := env[envKey]; ok {
			return v
		}
		return fileValue
	}
	c := jira.DiagnosticConfig{
		BaseURL:    choose(*base, "JIRA_URL", cfg.BaseURL, seen["url"] || seen["base-url"]),
		Email:      choose(*email, "JIRA_EMAIL", cfg.Email, seen["email"]),
		Deployment: choose(*deployment, "JIRA_DEPLOYMENT", cfg.Deployment, seen["deployment"]),
		CloudID:    choose(*cloudID, "JIRA_CLOUD_ID", cfg.CloudID, seen["cloud-id"]),
		TokenEnv:   choose(*tokenEnv, "JIRA_TOKEN_ENV", cfg.TokenEnv, seen["token-env"]),
	}
	if !seen["url"] && !seen["base-url"] && env["JIRA_URL"] == "" && env["JIRA_BASE_URL"] != "" {
		c.BaseURL = env["JIRA_BASE_URL"]
	}
	if c.Deployment != "" && c.Deployment != cfg.Deployment {
		if !seen["token-env"] && env["JIRA_TOKEN_ENV"] == "" {
			c.TokenEnv = ""
		}
		if c.Deployment == "datacenter" && !seen["cloud-id"] && env["JIRA_CLOUD_ID"] == "" {
			c.CloudID = ""
		}
	}
	if e := c.Validate(); e != nil {
		return fail(e)
	}
	*format = choose(*format, "JIRA_OUTPUT", cfg.Output, seen["output"] || seen["o"])
	if *format == "" {
		*format = "json"
	}
	if *format != "plain" && !validFormat(*format) {
		return fail(errors.New("invalid output format"))
	}
	if *timeout < 1 || *timeout > 120 {
		return fail(errors.New("timeout must be 1–120 seconds"))
	}
	required := 0
	switch cmd {
	case "project", "issue", "transitions", "request", "search":
		required = 1
	}
	if fs.NArg() != required {
		return fail(fmt.Errorf("%s requires %d positional arguments", cmd, required))
	}
	emit := func(v any) int {
		var e error
		if *format == "plain" {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			e = enc.Encode(v)
		} else {
			e = jira.Emit(out, *format, v)
		}
		if e != nil {
			fmt.Fprintln(errOut, e)
			return 1
		}
		return 0
	}
	token := env[c.TokenEnv]
	// Keep legacy Cloud config credentials usable, but never copy them on configure.
	if token == "" && c.TokenEnv == "JIRA_API_TOKEN" && c.Deployment == "cloud" {
		token = cfg.APIToken
	}
	if cmd == "configure" {
		cfg.BaseURL = c.BaseURL
		cfg.Email = c.Email
		cfg.Deployment = c.Deployment
		cfg.CloudID = c.CloudID
		cfg.TokenEnv = c.TokenEnv
		cfg.APIToken = ""
		cfg.Output = *format
		if cfg.Output == "plain" {
			cfg.Output = "json"
		}
		if e := saveDiagnosticConfig(env, cfg); e != nil {
			return fail(e)
		}
		return emit(map[string]any{"saved": true, "settings": c, "note": "Only non-secret settings saved; supply the token through its environment variable."})
	}
	if cmd == "status" {
		return emit(map[string]any{"settings": c, "tokenPresent": token != "", "authentication": map[string]string{"cloud": "email + API token", "datacenter": "personal access token (Bearer)"}[c.Deployment]})
	}
	if token == "" {
		fmt.Fprintf(errOut, "set %s before running diagnostics\n", c.TokenEnv)
		return 4
	}
	d, e := jira.NewDiagnostics(c, token, time.Duration(*timeout)*time.Second, env["JIRA_CA_BUNDLE"])
	if e != nil {
		return fail(e)
	}
	var r jira.DiagnosticResult
	ctx := context.Background()
	switch cmd {
	case "doctor":
		r, e = d.Doctor(ctx, *project, *issue)
	case "search":
		r, e = d.Search(ctx, fs.Arg(0), *limit)
	case "request":
		q := url.Values{}
		for _, p := range params {
			k, v, ok := strings.Cut(p, "=")
			if !ok || !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`).MatchString(k) || q.Has(k) {
				return fail(errors.New("parameters require unique KEY=VALUE entries"))
			}
			q.Set(k, v)
		}
		r, e = d.Request(ctx, fs.Arg(0), q)
	default:
		r, e = d.Inspect(ctx, cmd, fs.Arg(0), *project, *issue, *permissions)
	}
	if e != nil {
		return fail(e)
	}
	if code := emit(r); code != 0 {
		return code
	}
	return r.ExitCode()
}
func saveDiagnosticConfig(env map[string]string, cfg fileConfig) error {
	path, explicit := env["JIRA_CONFIG"]
	if explicit && (path == "" || path == "-") {
		return errors.New("configure requires an enabled config file")
	}
	if !explicit {
		if env["HOME"] == "" {
			return errors.New("HOME is not set")
		}
		path = filepath.Join(env["HOME"], ".go-jira", "go-jirarc")
	}
	dir := filepath.Dir(path)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return errors.New("cannot create config directory")
	}
	st, e := os.Lstat(dir)
	if e != nil || !st.IsDir() || st.Mode().Perm()&0022 != 0 {
		return errors.New("config directory must be a safe directory")
	}
	data, e := toml.Marshal(cfg)
	if e != nil {
		return e
	}
	if len(data) > maxConfigSize {
		return errors.New("configuration too large")
	}
	f, e := os.CreateTemp(dir, ".go-jirarc-*")
	if e != nil {
		return errors.New("cannot create config file")
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(data); e != nil {
		f.Close()
		return errors.New("cannot write config")
	}
	if e = f.Close(); e != nil {
		return errors.New("cannot close config")
	}
	if e = os.Rename(name, path); e != nil {
		return errors.New("cannot replace config")
	}
	return nil
}
