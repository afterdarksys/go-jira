package jira

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	testEmail = "user@example.com"
	testToken = "token-value-1"
)

func testClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, testEmail, testToken, WithLoopbackHTTP())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewRejectsUnsafeConfig(t *testing.T) {
	good := []Option{WithLoopbackHTTP()}
	cases := []struct {
		name    string
		base    string
		email   string
		token   string
		opts    []Option
		wantSub string
	}{
		{name: "http host", base: "http://example.com", email: testEmail, token: testToken, wantSub: "https"},
		{name: "http localhost name", base: "http://localhost:9", email: testEmail, token: testToken, opts: good, wantSub: "https"},
		{name: "http without option", base: "http://127.0.0.1:9", email: testEmail, token: testToken, wantSub: "https"},
		{name: "userinfo", base: "https://user:pass@example.com", email: testEmail, token: testToken, wantSub: "invalid base URL"},
		{name: "query", base: "https://example.com?x=1", email: testEmail, token: testToken, wantSub: "invalid base URL"},
		{name: "fragment", base: "https://example.com#x", email: testEmail, token: testToken, wantSub: "invalid base URL"},
		{name: "dotdot", base: "https://example.com/a/../b", email: testEmail, token: testToken, wantSub: "invalid base URL"},
		{name: "empty token", base: "https://example.com", email: testEmail, token: "", wantSub: "api token"},
		{name: "short token", base: "https://example.com", email: testEmail, token: "short", wantSub: "api token"},
		{name: "colon token", base: "https://example.com", email: testEmail, token: "token:value", wantSub: "api token"},
		{name: "bad email", base: "https://example.com", email: "not-an-email", token: testToken, wantSub: "email"},
		{name: "nil option", base: "https://example.com", email: testEmail, token: testToken, opts: []Option{nil}, wantSub: "nil option"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.base, tc.email, tc.token, tc.opts...)
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("got %v", err)
			}
			if strings.Contains(err.Error(), tc.token) && tc.token != "" {
				t.Fatalf("token leaked: %v", err)
			}
		})
	}

	if _, err := New("http://127.0.0.1:9", testEmail, testToken, WithLoopbackHTTP()); err != nil {
		t.Fatal(err)
	}
	if _, err := New("http://[::1]:9", testEmail, testToken, WithLoopbackHTTP()); err != nil {
		t.Fatal(err)
	}
}

func TestNewRejectsDisabledTLSVerify(t *testing.T) {
	hc := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // this is the rejected configuration
	}}
	_, err := New("https://example.com", testEmail, testToken, WithHTTPClient(hc))
	if err == nil || !strings.Contains(err.Error(), "tls verification") {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatal(err)
	}
}

func TestNewRejectsForeignTransport(t *testing.T) {
	hc := &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("should not be called")
	})}
	_, err := New("https://example.com", testEmail, testToken, WithHTTPClient(hc))
	if err == nil || !strings.Contains(err.Error(), "http.Transport") {
		t.Fatal(err)
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNewRequiresTLS13(t *testing.T) {
	hc := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}}
	c, err := New("https://example.com", testEmail, testToken, WithHTTPClient(hc))
	if err != nil {
		t.Fatal(err)
	}
	tr := c.http.Transport.(*http.Transport)
	if tr.TLSClientConfig.MinVersion != tls.VersionTLS13 {
		t.Fatalf("min version %x", tr.TLSClientConfig.MinVersion)
	}
	if tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("verification disabled")
	}
	if err := c.http.CheckRedirect(nil, nil); !errors.Is(err, ErrRedirect) {
		t.Fatal(err)
	}
	if c.http.Jar != nil {
		t.Fatal("cookie jar kept")
	}
}

func TestNewClearsCookieJarAndCapsTimeout(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = New("https://example.com", testEmail, testToken, WithHTTPClient(&http.Client{
		Jar:       jar,
		Timeout:   3 * time.Minute,
		Transport: &http.Transport{},
	}))
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatal(err)
	}
	c, err := New("https://example.com", testEmail, testToken, WithHTTPClient(&http.Client{
		Jar:       jar,
		Transport: &http.Transport{},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.http.Jar != nil {
		t.Fatal("jar")
	}
	if c.http.Timeout != httpTimeout {
		t.Fatalf("timeout %s", c.http.Timeout)
	}
}

func TestClientStringRedactsSecret(t *testing.T) {
	c, err := New("https://example.com", testEmail, testToken)
	if err != nil {
		t.Fatal(err)
	}
	basic := base64.StdEncoding.EncodeToString([]byte(testEmail + ":" + testToken))
	for _, s := range []string{c.String(), c.GoString(), fmt.Sprintf("%v %#v", c, c)} {
		if strings.Contains(s, testToken) || strings.Contains(s, basic) {
			t.Fatalf("secret in %s", s)
		}
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	var paths []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Basic ") {
			t.Errorf("missing auth on %s", r.URL.Path)
		}
		http.Redirect(w, r, srv.URL+"/steal?x="+testToken, http.StatusFound)
	}))
	defer srv.Close()
	c, err := New(srv.URL, testEmail, testToken, WithLoopbackHTTP(), WithHTTPClient(&http.Client{
		Timeout:   time.Second,
		Transport: &http.Transport{},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return nil
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.View(context.Background(), "PROJ-1")
	if err == nil {
		t.Fatal("expected redirect error")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("token in error: %v", err)
	}
	if len(paths) != 1 || !strings.HasPrefix(paths[0], "/rest/api/3/issue/PROJ-1") {
		t.Fatalf("paths %v", paths)
	}
}

func TestRedirectStatuses(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var n int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n++
				if r.URL.Path == "/next" {
					t.Error("followed")
				}
				w.Header().Set("Location", "/next")
				w.WriteHeader(status)
			}))
			defer srv.Close()
			c, err := New(srv.URL, testEmail, testToken, WithLoopbackHTTP())
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.View(context.Background(), "PROJ-1")
			if !errors.Is(err, ErrRedirect) {
				t.Fatal(err)
			}
			if n != 1 {
				t.Fatalf("hits %d", n)
			}
		})
	}
}

func TestResponseTooLarge(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytesOf('a', maxResponse+1))
	}))
	_, err := c.View(context.Background(), "PROJ-1")
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatal(err)
	}
}

func TestRequestTooLargeAndNilContext(t *testing.T) {
	called := false
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	err := c.do(context.Background(), http.MethodPost, "/rest/api/3/issue/PROJ-1/worklog", nil, strings.Repeat("a", maxRequest), nil)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatal(err)
	}
	if err := c.do(nil, http.MethodGet, "/rest/api/3/issue/PROJ-1", nil, nil, nil); err == nil {
		t.Fatal("nil context")
	}
	if err := c.do(context.Background(), http.MethodGet, "/rest/../admin", nil, nil, nil); err == nil {
		t.Fatal("dotdot path")
	}
	if called {
		t.Fatal("request was sent")
	}
}

func TestAPIErrorRedactsToken(t *testing.T) {
	token := `ab"cdEF12`
	basic := base64.StdEncoding.EncodeToString([]byte(testEmail + ":" + token))
	body, err := json.Marshal(map[string]any{
		"errorMessages": []string{"saw " + token + " and " + basic},
		"errors":        map[string]string{"summary": "nope " + token},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	c, err := New(srv.URL, testEmail, token, WithLoopbackHTTP())
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.View(context.Background(), "PROJ-1")
	if err == nil {
		t.Fatal("expected api error")
	}
	msg := err.Error()
	if strings.Contains(msg, token) || strings.Contains(msg, basic) {
		t.Fatalf("secret in error: %s", msg)
	}
	if !strings.Contains(msg, "[redacted]") || !strings.Contains(msg, "http 400") {
		t.Fatalf("error %s", msg)
	}
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatal(err)
	}
}

func TestUnparsedErrorBodyIsDropped(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "proxy said "+testToken)
	}))
	_, err := c.View(context.Background(), "PROJ-1")
	if err == nil || err.Error() != "jira: http 502" {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatal(err)
	}
}

func TestTLS12IsRejected(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("tls 1.2 handler reached")
	}))
	srv.TLS = &tls.Config{MaxVersion: tls.VersionTLS12}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	c := tlsClient(t, srv)
	_, err := c.View(context.Background(), "PROJ-1")
	if err == nil {
		t.Fatal("expected handshake failure")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatal(err)
	}
}

func TestTLS13Works(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/PROJ-1" {
			t.Errorf("path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"id":"1","key":"PROJ-1","fields":{"summary":"ok"}}`)
	}))
	t.Cleanup(srv.Close)
	c := tlsClient(t, srv)
	tr := c.http.Transport.(*http.Transport)
	if tr.TLSClientConfig.MinVersion != tls.VersionTLS13 || tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("tls config min=%x skip=%v", tr.TLSClientConfig.MinVersion, tr.TLSClientConfig.InsecureSkipVerify)
	}
	got, err := c.View(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != "PROJ-1" || got.Fields.Summary != "ok" {
		t.Fatalf("%+v", got)
	}
}

func tlsClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	c, err := New(srv.URL, testEmail, testToken, WithHTTPClient(&http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func bytesOf(b byte, n int) []byte {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = b
	}
	return buf
}
