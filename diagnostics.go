package jira

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// DiagnosticConfig contains non-secret connection settings.
type DiagnosticConfig struct {
	BaseURL    string `json:"url" toml:"base_url"`
	Deployment string `json:"deployment" toml:"deployment"`
	Email      string `json:"email,omitempty" toml:"email"`
	CloudID    string `json:"cloudId,omitempty" toml:"cloud_id"`
	TokenEnv   string `json:"tokenEnv" toml:"token_env"`
}

var diagID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var diagPath = regexp.MustCompile(`^/rest/api/[23]/[A-Za-z0-9_./~-]+$`)
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var authPattern = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[A-Za-z0-9+/_.=-]+`)
var keyNormalize = regexp.MustCompile(`[^a-z]`)

func secretKey(k string) bool {
	switch keyNormalize.ReplaceAllString(strings.ToLower(k), "") {
	case "authorization", "proxyauthorization", "cookie", "setcookie", "password", "secret", "clientsecret", "token", "accesstoken", "refreshtoken", "apitoken", "apikey":
		return true
	}
	return false
}

// Validate applies defaults and validates settings without connecting.
func (c *DiagnosticConfig) Validate() error {
	if c.Deployment == "" {
		c.Deployment = "cloud"
	}
	if c.Deployment != "cloud" && c.Deployment != "datacenter" {
		return errors.New("deployment must be cloud or datacenter")
	}
	if c.TokenEnv == "" {
		c.TokenEnv = "JIRA_API_TOKEN"
		if c.Deployment == "datacenter" {
			c.TokenEnv = "JIRA_PAT"
		}
	}
	if !envName.MatchString(c.TokenEnv) {
		return errors.New("invalid token environment variable")
	}
	u, err := parseBase(c.BaseURL, false)
	if err != nil {
		return err
	}
	if strings.ContainsAny(c.BaseURL, "\\%?#") {
		return errors.New("invalid diagnostic base URL")
	}
	for _, s := range strings.Split(u.Path, "/") {
		if s == "." || s == ".." {
			return errors.New("invalid base path")
		}
	}
	if c.Deployment == "cloud" && (!validEmail(c.Email) || strings.Contains(c.Email, ":")) {
		return errors.New("Cloud diagnostics require an email")
	}
	if c.CloudID != "" && (c.Deployment != "cloud" || !regexp.MustCompile(`^[A-Za-z0-9-]{1,128}$`).MatchString(c.CloudID)) {
		return errors.New("invalid Cloud ID")
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	return nil
}

// Diagnostics is a GET-only client for Cloud v3 and Data Center v2.
type Diagnostics struct {
	client     *Client
	api        string
	deployment string
}

// NewDiagnostics builds a verified HTTPS client. caBundle optionally adds PEM roots.
func NewDiagnostics(cfg DiagnosticConfig, token string, timeout time.Duration, caBundle string) (*Diagnostics, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if timeout < time.Second || timeout > 120*time.Second {
		return nil, errors.New("timeout must be between 1 and 120 seconds")
	}
	base := cfg.BaseURL
	if cfg.CloudID != "" {
		base = "https://api.atlassian.com/ex/jira/" + cfg.CloudID
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("unsupported default HTTP transport")
	}
	tr := transport.Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS13}
	if caBundle != "" {
		pem, err := os.ReadFile(caBundle)
		if err != nil {
			return nil, errors.New("could not load Jira CA bundle")
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("invalid Jira CA bundle")
		}
		tr.TLSClientConfig.RootCAs = roots
	}
	email := cfg.Email
	if cfg.Deployment == "datacenter" {
		email = "pat@example.com"
	}
	c, err := New(base, email, token, WithHTTPClient(&http.Client{Timeout: timeout, Transport: tr}))
	if err != nil {
		return nil, err
	}
	api := "/rest/api/3"
	if cfg.Deployment == "datacenter" {
		api = "/rest/api/2"
		c.authorize = func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
	}
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Diagnostics{c, api, cfg.Deployment}, nil
}

// DiagnosticResult preserves HTTP and network failures as structured output.
type DiagnosticResult map[string]any

// ExitCode distinguishes authentication (4), transient (5), and other failures (1).
func (r DiagnosticResult) ExitCode() int {
	if ok, _ := r["ok"].(bool); ok {
		return 0
	}
	code := 1
	visit := func(row map[string]any) {
		s, _ := row["status"].(int)
		kind, _ := row["errorType"].(string)
		if s == 401 || s == 403 {
			code = 4
		}
		if code != 4 && (s == 429 || s == 500 || s == 502 || s == 503 || s == 504 || kind == "connection" || kind == "timeout") {
			code = 5
		}
	}
	visit(r)
	if checks, ok := r["checks"].([]DiagnosticResult); ok {
		for _, c := range checks {
			visit(c)
		}
	}
	return code
}
func diagnosis(s int) string {
	if s >= 200 && s < 300 {
		return "Request succeeded."
	}
	if s >= 300 && s < 400 {
		return "Redirect refused; check base URL, context path, SSO and proxy routing."
	}
	switch s {
	case 400:
		return "Invalid parameters or JQL; inspect validation errors."
	case 401:
		return "Authentication failed; check token expiry, identity, deployment and scoped-token routing."
	case 403:
		return "Access denied; check account access, token scopes, project permissions and organization policy."
	case 404:
		return "Resource missing, hidden by permissions, or addressed through the wrong API/base URL."
	case 405:
		return "Endpoint does not support GET; diagnostics never execute writes."
	case 429:
		return "Rate limited; inspect Retry-After and rate-limit headers. No automatic retries."
	}
	return "Jira or proxy error; correlate request IDs with server logs."
}
func (d *Diagnostics) scrub(v any) any {
	switch x := v.(type) {
	case string:
		return authPattern.ReplaceAllString(d.client.redact(x), "$1 [redacted]")
	case map[string]any:
		out := map[string]any{}
		for k, v := range x {
			key := d.scrub(k).(string)
			if secretKey(k) {
				out[key] = "[redacted]"
			} else {
				out[key] = d.scrub(v)
			}
		}
		return out
	case []any:
		for i := range x {
			x[i] = d.scrub(x[i])
		}
		return x
	}
	return v
}

// Request only permits API v2/v3 paths and never prints query values or raw HTML.
func (d *Diagnostics) Request(ctx context.Context, path string, q url.Values) (DiagnosticResult, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if !diagPath.MatchString(path) || strings.Contains(path, "//") {
		return nil, errors.New("request path must stay under /rest/api/2/ or /rest/api/3/")
	}
	for _, s := range strings.Split(path, "/") {
		if s == "." || s == ".." {
			return nil, errors.New("path traversal refused")
		}
	}
	keys := []string{}
	for k := range q {
		if secretKey(k) {
			return nil, errors.New("credentials must not be query parameters")
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	u := *d.client.base
	u.Path += path
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, errors.New("invalid diagnostic request")
	}
	d.client.authorize(req)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "go-jira-diagnostics")
	start := time.Now()
	r := DiagnosticResult{"ok": false, "trace": map[string]any{"method": "GET", "path": path, "queryKeys": keys}}
	resp, err := d.client.http.Do(req)
	if err != nil {
		kind := "connection"
		var ne net.Error
		var ce *tls.CertificateVerificationError
		if errors.As(err, &ce) {
			kind = "tls"
		} else if errors.As(err, &ne) && ne.Timeout() {
			kind = "timeout"
		}
		r["errorType"] = kind
		r["diagnosis"] = "Request failed (" + kind + "); check DNS, VPN, proxy, certificates/JIRA_CA_BUNDLE or timeout."
	} else {
		defer resp.Body.Close()
		r["status"] = resp.StatusCode
		r["ok"] = resp.StatusCode >= 200 && resp.StatusCode < 300
		r["diagnosis"] = diagnosis(resp.StatusCode)
		headers := map[string]any{}
		for _, k := range []string{"Content-Type", "Retry-After", "X-Arequestid", "Atl-Traceid", "X-Request-Id", "X-Ratelimit-Limit", "X-Ratelimit-Remaining", "X-Ratelimit-Reset", "Ratelimit-Reason"} {
			if v := resp.Header.Get(k); v != "" {
				headers[strings.ToLower(k)] = v
			}
		}
		r["headers"] = headers
		body, e := io.ReadAll(io.LimitReader(resp.Body, (5<<20)+1))
		switch {
		case e != nil:
			r["ok"] = false
			r["errorType"] = "connection"
			r["diagnosis"] = "Failed reading response."
		case len(body) > 5<<20:
			r["ok"] = false
			r["errorType"] = "response-too-large"
			r["diagnosis"] = "Response exceeds 5 MiB; request fewer fields/results."
		case len(body) > 0:
			var v any
			if json.Unmarshal(body, &v) != nil {
				r["ok"] = false
				r["errorType"] = "non-json"
				r["diagnosis"] = diagnosis(resp.StatusCode) + " Expected JSON; check SSO/proxy routing. Body omitted."
			} else {
				r["data"] = v
			}
		}
	}
	r["elapsedMs"] = time.Since(start).Milliseconds()
	return DiagnosticResult(d.scrub(map[string]any(r)).(map[string]any)), nil
}
