package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testDiagnostics(t *testing.T, deployment string, h http.HandlerFunc) *Diagnostics {
	t.Helper()
	s := httptest.NewTLSServer(h)
	t.Cleanup(s.Close)
	d, e := NewDiagnostics(DiagnosticConfig{BaseURL: s.URL + "/jira", Email: "user@example.com", Deployment: deployment}, "super-secret", time.Second, "")
	if e != nil {
		t.Fatal(e)
	}
	hc, e := buildHTTP(s.Client())
	if e != nil {
		t.Fatal(e)
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	d.client.http = hc
	return d
}
func TestDiagnosticsAuthPathsAndRedaction(t *testing.T) {
	for _, deployment := range []string{"cloud", "datacenter"} {
		t.Run(deployment, func(t *testing.T) {
			d := testDiagnostics(t, deployment, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Fatal(r.Method)
				}
				version := "3"
				if deployment == "datacenter" {
					version = "2"
					if r.Header.Get("Authorization") != "Bearer super-secret" {
						t.Error("PAT auth missing")
					}
				} else {
					u, p, ok := r.BasicAuth()
					if !ok || u != "user@example.com" || p != "super-secret" {
						t.Error("basic auth missing")
					}
				}
				if r.URL.Path != "/jira/rest/api/"+version+"/issue/SEC-1/transitions" {
					t.Error(r.URL.Path)
				}
				w.Header().Set("Set-Cookie", "hidden")
				w.Header().Set("X-Request-Id", "trace")
				fmt.Fprint(w, `{"token":"hidden","nested":[{"message":"super-secret Bearer abc123"}]}`)
			})
			r, e := d.Inspect(context.Background(), "transitions", "SEC-1", "", "", "")
			if e != nil || r.ExitCode() != 0 {
				t.Fatalf("%v %v", r, e)
			}
			b, _ := json.Marshal(r)
			for _, secret := range []string{"super-secret", "hidden", "abc123", "set-cookie"} {
				if strings.Contains(string(b), secret) {
					t.Fatalf("leak: %s", b)
				}
			}
		})
	}
}
func TestDiagnosticFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		kind   string
		code   int
	}{{"auth", 401, `{"errorMessages":["no"]}`, "", 4}, {"rate", 429, `{}`, "", 5}, {"redirect", 302, `{}`, "", 1}, {"html", 200, "<html>secret</html>", "non-json", 1}, {"large", 200, strings.Repeat("x", (5<<20)+1), "response-too-large", 1}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			d := testDiagnostics(t, "cloud", func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "https://other.example")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			r, e := d.Request(context.Background(), "/rest/api/3/myself", nil)
			if e != nil || r.ExitCode() != tc.code || calls != 1 {
				t.Fatalf("%v %v calls=%d", r, e, calls)
			}
			if tc.kind != "" && r["errorType"] != tc.kind {
				t.Fatal(r)
			}
		})
	}
}
func TestDiagnosticRequestValidation(t *testing.T) {
	d := testDiagnostics(t, "cloud", func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected request") })
	for _, path := range []string{"https://evil.example", "//evil", "/rest/api/3/../myself", "/rest/api/3/%2e%2e/myself", "/rest/api/3/myself?token=x", "/rest/api/3/./myself"} {
		if _, e := d.Request(context.Background(), path, nil); e == nil {
			t.Error(path)
		}
	}
	if _, e := d.Request(context.Background(), "/rest/api/3/myself", url.Values{"api_token": {"secret"}}); e == nil {
		t.Fatal("credential query allowed")
	}
	if _, e := d.Inspect(context.Background(), "project", "../bad", "", "", ""); e == nil {
		t.Fatal("bad identifier allowed")
	}
}
func TestDiagnosticSearchPagination(t *testing.T) {
	for _, dep := range []string{"cloud", "datacenter"} {
		t.Run(dep, func(t *testing.T) {
			calls := 0
			d := testDiagnostics(t, dep, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Query().Get("jql") != "project=SEC" {
					t.Error("JQL missing")
				}
				if dep == "cloud" {
					if calls == 1 {
						fmt.Fprint(w, `{"issues":[{"key":"SEC-1"}],"nextPageToken":"next"}`)
					} else {
						if r.URL.Query().Get("nextPageToken") != "next" {
							t.Error("cursor missing")
						}
						fmt.Fprint(w, `{"issues":[{"key":"SEC-2"}],"isLast":true}`)
					}
				} else {
					if r.URL.Query().Get("startAt") != fmt.Sprint(calls-1) {
						t.Error("offset missing")
					}
					fmt.Fprint(w, `{"issues":[{"key":"SEC-1"}],"total":2}`)
				}
			})
			r, e := d.Search(context.Background(), "project=SEC", 2)
			if e != nil || r.ExitCode() != 0 || r["count"] != 2 || r["truncated"] != false || calls != 2 {
				t.Fatalf("%v %v calls=%d", r, e, calls)
			}
		})
	}
}
func TestDiagnosticSearchPartialAndLoops(t *testing.T) {
	for _, mode := range []string{"failure", "loop", "empty", "shape"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			d := testDiagnostics(t, "cloud", func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 1 {
					fmt.Fprint(w, `{"issues":[{}],"nextPageToken":"same"}`)
					return
				}
				switch mode {
				case "failure":
					w.WriteHeader(503)
					fmt.Fprint(w, `{}`)
				case "loop":
					fmt.Fprint(w, `{"issues":[{}],"nextPageToken":"same"}`)
				case "empty":
					fmt.Fprint(w, `{"issues":[],"nextPageToken":"new"}`)
				case "shape":
					fmt.Fprint(w, `{"issues":{}}`)
				}
			})
			r, e := d.Search(context.Background(), "project=SEC", 10)
			if e != nil || r["ok"] != false || r["partial"] != true || calls != 2 {
				t.Fatalf("%v %v calls=%d", r, e, calls)
			}
			if mode == "failure" && r.ExitCode() != 5 {
				t.Fatal(r)
			}
		})
	}
}
func TestDiagnosticDoctorAndConfig(t *testing.T) {
	calls := 0
	d := testDiagnostics(t, "datacenter", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if strings.HasSuffix(r.URL.Path, "myself") {
			w.WriteHeader(403)
		}
		fmt.Fprint(w, `{}`)
	})
	r, e := d.Doctor(context.Background(), "SEC", "SEC-1")
	if e != nil || calls != 5 || r.ExitCode() != 4 {
		t.Fatalf("%v %v", r, e)
	}
	for _, c := range []DiagnosticConfig{{BaseURL: "http://example.com", Email: "a@b.com"}, {BaseURL: "https://example.com/./jira", Email: "a@b.com"}, {BaseURL: "https://example.com", Deployment: "datacenter", CloudID: "abc"}} {
		if c.Validate() == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
	c := DiagnosticConfig{BaseURL: "https://example.com", Email: "a@b.com", CloudID: "abc"}
	d, e = NewDiagnostics(c, "test-token", time.Second, "")
	if e != nil || d.client.base.String() != "https://api.atlassian.com/ex/jira/abc" {
		t.Fatalf("%v %v", d, e)
	}
}
