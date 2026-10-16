package jira

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxResponse = 8 << 20
	maxRequest  = 1 << 20
	maxError    = 64 << 10
	httpTimeout = 30 * time.Second
	maxTimeout  = 2 * time.Minute
)

// ErrRedirect is returned when Jira responds with a redirect.
// Redirects are not followed, so the API token is not sent to another host.
var ErrRedirect = errors.New("jira: redirects are not followed")

// ErrResponseTooLarge is returned when a response exceeds 8 MiB.
var ErrResponseTooLarge = errors.New("jira: response exceeds 8MiB")

// APIError is a non-2xx response from Jira.
type APIError struct {
	Status   int
	Messages []string
	Errors   map[string]string
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "jira: http %d", e.Status)
	for _, m := range e.Messages {
		b.WriteString(": ")
		b.WriteString(m)
	}
	for k, v := range e.Errors {
		fmt.Fprintf(&b, ": %s: %s", k, v)
	}
	return b.String()
}

// Option configures a Client.
type Option func(*options)

type options struct {
	hc                *http.Client
	allowLoopbackHTTP bool
}

// WithHTTPClient uses hc for requests. Certificate verification cannot be
// disabled. Redirects are still refused, and the timeout is capped at 2 minutes.
func WithHTTPClient(hc *http.Client) Option {
	return func(o *options) { o.hc = hc }
}

// WithLoopbackHTTP allows an http base URL only when the host is a loopback IP.
// It does not disable TLS verification. Production sites stay on https.
func WithLoopbackHTTP() Option {
	return func(o *options) { o.allowLoopbackHTTP = true }
}

// Client calls the Jira Cloud REST API v3.
// The API token is held in a closure so formatting the client does not print it.
type Client struct {
	base      *url.URL
	http      *http.Client
	authorize func(*http.Request)
	redact    func(string) string
}

// String redacts credentials.
func (c *Client) String() string {
	if c == nil || c.base == nil {
		return "jira.Client"
	}
	return "jira.Client{baseURL:" + c.base.String() + "}"
}

// GoString redacts credentials.
func (c *Client) GoString() string { return c.String() }

// New builds a client. baseURL is the site origin, email is the Atlassian
// account email, and token is an API token. The token is sent as HTTP Basic.
func New(baseURL, email, token string, opts ...Option) (*Client, error) {
	var o options
	for _, opt := range opts {
		if opt == nil {
			return nil, errors.New("jira: nil option")
		}
		opt(&o)
	}
	u, err := parseBase(baseURL, o.allowLoopbackHTTP)
	if err != nil {
		return nil, err
	}
	if !validEmail(email) {
		return nil, errors.New("jira: invalid email")
	}
	if !validToken(token) {
		return nil, errors.New("jira: invalid api token")
	}
	hc, err := buildHTTP(o.hc)
	if err != nil {
		return nil, err
	}
	basic := base64.StdEncoding.EncodeToString([]byte(email + ":" + token))
	return &Client{
		base: u,
		http: hc,
		authorize: func(r *http.Request) {
			r.Header.Set("Authorization", "Basic "+basic)
		},
		redact: func(s string) string {
			if token != "" {
				s = strings.ReplaceAll(s, token, "[redacted]")
			}
			if basic != "" {
				s = strings.ReplaceAll(s, basic, "[redacted]")
			}
			return s
		},
	}, nil
}

func parseBase(raw string, allowLoopbackHTTP bool) (*url.URL, error) {
	if len(raw) == 0 || len(raw) > 256 {
		return nil, errors.New("jira: invalid base URL")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("jira: invalid base URL")
	}
	if strings.Contains(u.Path, "..") {
		return nil, errors.New("jira: invalid base URL")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !allowLoopbackHTTP || !isLoopback(u.Hostname()) {
			return nil, errors.New("jira: base URL must be https")
		}
	default:
		return nil, errors.New("jira: base URL must be https")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}

func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func buildHTTP(hc *http.Client) (*http.Client, error) {
	if hc == nil {
		base, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, errors.New("jira: unsupported default transport")
		}
		tr := base.Clone()
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS13}
		tr.DisableCompression = true
		return &http.Client{
			Timeout:       httpTimeout,
			Transport:     tr,
			CheckRedirect: refuseRedirect,
		}, nil
	}
	if hc.Timeout < 0 || hc.Timeout > maxTimeout {
		return nil, errors.New("jira: http timeout must be between 0 and 2m")
	}
	out := *hc
	out.Jar = nil
	if out.Timeout == 0 {
		out.Timeout = httpTimeout
	}
	out.CheckRedirect = refuseRedirect
	if out.Transport == nil {
		base, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, errors.New("jira: unsupported default transport")
		}
		out.Transport = base.Clone()
	}
	tr, ok := out.Transport.(*http.Transport)
	if !ok {
		return nil, errors.New("jira: http transport must be *http.Transport")
	}
	cloned := tr.Clone()
	var cfg *tls.Config
	if cloned.TLSClientConfig != nil {
		if cloned.TLSClientConfig.InsecureSkipVerify {
			return nil, errors.New("jira: tls verification cannot be disabled")
		}
		cfg = cloned.TLSClientConfig.Clone()
	} else {
		cfg = &tls.Config{}
	}
	if cfg.MinVersion < tls.VersionTLS13 {
		cfg.MinVersion = tls.VersionTLS13
	}
	cfg.InsecureSkipVerify = false
	cloned.TLSClientConfig = cfg
	cloned.DisableCompression = true
	out.Transport = cloned
	return &out, nil
}

func refuseRedirect(*http.Request, []*http.Request) error {
	return ErrRedirect
}

func (c *Client) do(ctx context.Context, method, pth string, q url.Values, body any, out any) error {
	if ctx == nil {
		return errors.New("jira: nil context")
	}
	if !strings.HasPrefix(pth, "/") || strings.Contains(pth, "..") {
		return errors.New("jira: invalid path")
	}
	u := *c.base
	u.Path = c.base.Path + pth
	if len(q) > 0 {
		u.RawQuery = q.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		if len(b) > maxRequest {
			return errors.New("jira: request body too large")
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rdr)
	if err != nil {
		return err
	}
	c.authorize(req)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("jira: %s %s: %w", method, pth, redactError(c.redact, err))
	}
	defer resp.Body.Close()
	buf, err := readLimited(resp.Body, maxResponse)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.apiError(resp.StatusCode, buf)
	}
	if out == nil || len(bytes.TrimSpace(buf)) == 0 {
		return nil
	}
	if err := json.Unmarshal(buf, out); err != nil {
		return fmt.Errorf("jira: decode %s %s: %w", method, pth, err)
	}
	return nil
}

func redactError(redact func(string) string, err error) error {
	if err == nil {
		return nil
	}
	msg := redact(err.Error())
	if msg == err.Error() {
		return err
	}
	return errors.New(msg)
}

func readLimited(r io.Reader, n int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, n+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > n {
		return nil, ErrResponseTooLarge
	}
	return b, nil
}

func (c *Client) apiError(status int, body []byte) error {
	if len(body) > maxError {
		body = body[:maxError]
	}
	ae := &APIError{Status: status}
	var parsed struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	// Redact after JSON parsing so escaped token characters are still caught.
	if json.Unmarshal(body, &parsed) == nil {
		if len(parsed.ErrorMessages) > 20 {
			parsed.ErrorMessages = parsed.ErrorMessages[:20]
		}
		for _, m := range parsed.ErrorMessages {
			ae.Messages = append(ae.Messages, clip(c.redact(m), 500))
		}
		if len(parsed.Errors) > 0 {
			ae.Errors = make(map[string]string, len(parsed.Errors))
			n := 0
			for k, v := range parsed.Errors {
				if n == 20 {
					break
				}
				ae.Errors[clip(c.redact(k), 80)] = clip(c.redact(v), 500)
				n++
			}
		}
	}
	return ae
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
