package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type captured struct {
	method string
	path   string
	query  string
	auth   string
	accept string
	ctype  string
	body   []byte
	hits   int
}

func captureClient(t *testing.T, status int, resp string) (*Client, *captured) {
	t.Helper()
	got := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		got.hits++
		got.method = r.Method
		got.path = r.URL.Path
		got.query = r.URL.RawQuery
		got.auth = r.Header.Get("Authorization")
		got.accept = r.Header.Get("Accept")
		got.ctype = r.Header.Get("Content-Type")
		got.body = b
		w.WriteHeader(status)
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, testEmail, testToken, WithLoopbackHTTP())
	if err != nil {
		t.Fatal(err)
	}
	return c, got
}

func assertAuth(t *testing.T, got *captured) {
	t.Helper()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(testEmail+":"+testToken))
	if got.auth != want {
		t.Fatal("authorization header mismatch")
	}
	if got.accept != "application/json" {
		t.Fatalf("accept %s", got.accept)
	}
	if strings.Contains(got.path, testToken) || strings.Contains(got.query, testToken) {
		t.Fatal("token in url")
	}
}

func asMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body %s: %v", body, err)
	}
	return m
}

func TestView(t *testing.T) {
	const body = `{"id":"100","key":"PROJ-1","fields":{"summary":"Ship","labels":["a"],"duedate":"2026-10-16","description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Hello"}]}]},"customfield_10001":"keep-me","timetracking":{"timeSpent":"1h","timeSpentSeconds":3600}}}`
	c, got := captureClient(t, 200, body)
	issue, err := c.View(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatal(err)
	}
	assertAuth(t, got)
	if got.method != http.MethodGet || got.path != "/rest/api/3/issue/PROJ-1" {
		t.Fatalf("%s %s", got.method, got.path)
	}
	if got.ctype != "" {
		t.Fatalf("content-type %s", got.ctype)
	}
	if issue.Key != "PROJ-1" || issue.Fields.Summary != "Ship" || issue.Fields.Description != "Hello" {
		t.Fatalf("%+v", issue.Fields)
	}
	if issue.Fields.DueDate != "2026-10-16" || len(issue.Fields.Labels) != 1 || issue.Fields.Labels[0] != "a" {
		t.Fatalf("%+v", issue.Fields)
	}
	if issue.Fields.TimeTracking == nil || issue.Fields.TimeTracking.TimeSpent != "1h" {
		t.Fatal(issue.Fields.TimeTracking)
	}
	raw := string(issue.RawJSON())
	if !strings.Contains(raw, "keep-me") {
		t.Fatal(raw)
	}

	c, got = captureClient(t, 200, `{"id":"9","key":"10001","fields":{"summary":"n","description":"wiki text"}}`)
	issue, err = c.View(context.Background(), "10001")
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/rest/api/3/issue/10001" || issue.Fields.Description != "wiki text" {
		t.Fatalf("%s %q", got.path, issue.Fields.Description)
	}
}

func TestViewBasePath(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		fmtJSON(w, `{"id":"1","key":"PROJ-1","fields":{"summary":"s"}}`)
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL+"/jira/", testEmail, testToken, WithLoopbackHTTP())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.View(context.Background(), "PROJ-1"); err != nil {
		t.Fatal(err)
	}
	if path != "/jira/rest/api/3/issue/PROJ-1" {
		t.Fatal(path)
	}
}

func TestUpdate(t *testing.T) {
	summary := "Renamed"
	desc := "Body"
	due := "2026-12-01"
	orig := "1d 4h"
	remain := "30"
	c, got := captureClient(t, 204, "")
	err := c.Update(context.Background(), "PROJ-1", Update{
		Summary:           &summary,
		Description:       &desc,
		Due:               &due,
		OriginalEstimate:  &orig,
		RemainingEstimate: &remain,
		TimeSpent:         "2h",
		TimeComment:       "worked",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertAuth(t, got)
	if got.method != http.MethodPut || got.path != "/rest/api/3/issue/PROJ-1" {
		t.Fatalf("%s %s", got.method, got.path)
	}
	if got.ctype != "application/json" {
		t.Fatal(got.ctype)
	}
	m := asMap(t, got.body)
	fields := m["fields"].(map[string]any)
	if fields["summary"] != "Renamed" || fields["duedate"] != "2026-12-01" {
		t.Fatal(fields)
	}
	doc := fields["description"].(map[string]any)
	if doc["type"] != "doc" || doc["version"] != float64(1) {
		t.Fatal(doc)
	}
	track := fields["timetracking"].(map[string]any)
	if track["originalEstimate"] != "1d4h" || track["remainingEstimate"] != "30m" {
		t.Fatal(track)
	}
	upd := m["update"].(map[string]any)
	work := upd["worklog"].([]any)[0].(map[string]any)["add"].(map[string]any)
	if work["timeSpent"] != "2h" {
		t.Fatal(work)
	}
	if work["comment"].(map[string]any)["type"] != "doc" {
		t.Fatal(work)
	}
}

func TestUpdateLabelsAndClear(t *testing.T) {
	c, got := captureClient(t, 204, "")
	empty := ""
	err := c.Update(context.Background(), "PROJ-1", Update{
		Description: &empty,
		SetLabels:   true,
		ClearDue:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := asMap(t, got.body)
	fields := m["fields"].(map[string]any)
	if fields["description"] != nil {
		t.Fatal(fields["description"])
	}
	if fields["duedate"] != nil {
		t.Fatal(fields["duedate"])
	}
	labels, ok := fields["labels"].([]any)
	if !ok || len(labels) != 0 {
		t.Fatalf("%#v", fields["labels"])
	}

	c, got = captureClient(t, 204, "")
	err = c.Update(context.Background(), "PROJ-1", Update{AddLabels: []string{"one"}, RemoveLabels: []string{"two"}})
	if err != nil {
		t.Fatal(err)
	}
	m = asMap(t, got.body)
	if _, ok := m["fields"]; ok {
		t.Fatal(m)
	}
	ops := m["update"].(map[string]any)["labels"].([]any)
	if len(ops) != 2 {
		t.Fatal(ops)
	}
}

func TestUpdateRejectsConflicts(t *testing.T) {
	called := false
	c := testClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	due := "2026-01-02"
	ctx := context.Background()
	bad := []Update{
		{},
		{SetLabels: true, AddLabels: []string{"a"}},
		{Due: &due, ClearDue: true},
		{TimeComment: "note"},
		{Labels: []string{"a", "a"}, SetLabels: true},
		{AddLabels: []string{"has space"}},
	}
	for i, u := range bad {
		if err := c.Update(ctx, "PROJ-1", u); err == nil {
			t.Fatalf("case %d", i)
		}
	}
	if _, err := c.View(ctx, "nope"); err == nil {
		t.Fatal("expected invalid issue")
	}
	if err := c.Assign(ctx, "PROJ-1", "bad id"); err == nil {
		t.Fatal("expected invalid account")
	}
	if called {
		t.Fatal("request sent")
	}
}

func TestAssignAndUnassign(t *testing.T) {
	c, got := captureClient(t, 204, "")
	if err := c.Assign(context.Background(), "PROJ-1", "acct-1"); err != nil {
		t.Fatal(err)
	}
	assertAuth(t, got)
	if got.method != http.MethodPut || got.path != "/rest/api/3/issue/PROJ-1/assignee" {
		t.Fatalf("%s %s", got.method, got.path)
	}
	m := asMap(t, got.body)
	if m["accountId"] != "acct-1" {
		t.Fatal(m)
	}

	c, got = captureClient(t, 204, "")
	if err := c.Unassign(context.Background(), "PROJ-1"); err != nil {
		t.Fatal(err)
	}
	m = asMap(t, got.body)
	if _, ok := m["accountId"]; !ok || m["accountId"] != nil {
		t.Fatal(m)
	}

	c, got = captureClient(t, 204, "")
	if err := c.Assign(context.Background(), "PROJ-1", "acct-2"); err != nil {
		t.Fatal(err)
	}
	if asMap(t, got.body)["accountId"] != "acct-2" {
		t.Fatal(got.body)
	}
}

func TestWatchers(t *testing.T) {
	c, got := captureClient(t, 200, `{"watchCount":1,"isWatching":true,"watchers":[{"accountId":"acct-1","displayName":"Ada"}]}`)
	list, err := c.Watchers(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatal(err)
	}
	assertAuth(t, got)
	if got.path != "/rest/api/3/issue/PROJ-1/watchers" || list.WatchCount != 1 || list.Watchers[0].AccountID != "acct-1" {
		t.Fatalf("%s %+v", got.path, list)
	}

	c, got = captureClient(t, 204, "")
	if err := c.AddWatcher(context.Background(), "PROJ-1", "acct-1"); err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || string(got.body) != `"acct-1"` {
		t.Fatalf("%s %s", got.method, got.body)
	}

	c, got = captureClient(t, 204, "")
	if err := c.AddWatcher(context.Background(), "PROJ-1", ""); err != nil {
		t.Fatal(err)
	}
	if len(got.body) != 0 || got.ctype != "" {
		t.Fatalf("self watch body %q type %s", got.body, got.ctype)
	}

	c, got = captureClient(t, 204, "")
	if err := c.RemoveWatcher(context.Background(), "PROJ-1", "acct-1"); err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodDelete {
		t.Fatal(got.method)
	}
	q, err := url.ParseQuery(got.query)
	if err != nil || q.Get("accountId") != "acct-1" {
		t.Fatal(got.query)
	}
}

func TestWorklogs(t *testing.T) {
	c, got := captureClient(t, 200, `{"startAt":0,"maxResults":100,"total":1,"worklogs":[{"id":"42","timeSpent":"1h","timeSpentSeconds":3600}]}`)
	page, err := c.Worklogs(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatal(err)
	}
	assertAuth(t, got)
	q, err := url.ParseQuery(got.query)
	if err != nil || q.Get("startAt") != "0" || q.Get("maxResults") != "100" {
		t.Fatal(got.query)
	}
	if page.Total != 1 || page.Worklogs[0].ID != "42" {
		t.Fatalf("%+v", page)
	}

	started := time.Date(2020, 1, 2, 3, 4, 5, 0, time.FixedZone("E", 3600))
	c, got = captureClient(t, 201, `{"id":"7","timeSpent":"2h","timeSpentSeconds":7200}`)
	gotLog, err := c.AddWorklog(context.Background(), "PROJ-1", WorklogInput{
		TimeSpent:      "1d 30m",
		Comment:        "notes",
		Started:        &started,
		AdjustEstimate: "manual",
		ReduceBy:       "30m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.path != "/rest/api/3/issue/PROJ-1/worklog" {
		t.Fatalf("%s %s", got.method, got.path)
	}
	q, _ = url.ParseQuery(got.query)
	if q.Get("adjustEstimate") != "manual" || q.Get("reduceBy") != "30m" {
		t.Fatal(got.query)
	}
	m := asMap(t, got.body)
	if m["timeSpent"] != "1d30m" || m["started"] != "2020-01-02T02:04:05.000+0000" {
		t.Fatal(m)
	}
	if gotLog.ID != "7" {
		t.Fatal(gotLog)
	}

	c, got = captureClient(t, 200, `{"id":"7","timeSpentSeconds":60}`)
	_, err = c.UpdateWorklog(context.Background(), "PROJ-1", "7", WorklogInput{
		TimeSpentSeconds: 60,
		AdjustEstimate:   "new",
		NewEstimate:      "1h",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPut || got.path != "/rest/api/3/issue/PROJ-1/worklog/7" {
		t.Fatalf("%s %s", got.method, got.path)
	}
	q, _ = url.ParseQuery(got.query)
	if q.Get("adjustEstimate") != "new" || q.Get("newEstimate") != "1h" {
		t.Fatal(got.query)
	}
	if asMap(t, got.body)["timeSpentSeconds"] != float64(60) {
		t.Fatal(got.body)
	}

	c, got = captureClient(t, 204, "")
	if err := c.DeleteWorklog(context.Background(), "PROJ-1", "7", "leave"); err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodDelete || got.path != "/rest/api/3/issue/PROJ-1/worklog/7" {
		t.Fatalf("%s %s", got.method, got.path)
	}
	q, _ = url.ParseQuery(got.query)
	if q.Get("adjustEstimate") != "leave" {
		t.Fatal(got.query)
	}

	c, got = captureClient(t, 204, "")
	if err := c.DeleteWorklog(context.Background(), "PROJ-1", "7", "auto"); err != nil {
		t.Fatal(err)
	}
	if got.query != "" {
		t.Fatal(got.query)
	}
}

func TestWorklogRejects(t *testing.T) {
	called := false
	c := testClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	ctx := context.Background()
	if _, err := c.AddWorklog(ctx, "PROJ-1", WorklogInput{}); err == nil {
		t.Fatal("empty")
	}
	if _, err := c.AddWorklog(ctx, "PROJ-1", WorklogInput{TimeSpent: "1h", TimeSpentSeconds: 5}); err == nil {
		t.Fatal("both")
	}
	if _, err := c.AddWorklog(ctx, "PROJ-1", WorklogInput{TimeSpent: "1h", AdjustEstimate: "nope"}); err == nil {
		t.Fatal("adjust")
	}
	if _, err := c.UpdateWorklog(ctx, "PROJ-1", "abc", WorklogInput{TimeSpent: "1h"}); err == nil {
		t.Fatal("id")
	}
	if err := c.DeleteWorklog(ctx, "PROJ-1", "7", "manual"); err == nil {
		t.Fatal("delete adjust")
	}
	if called {
		t.Fatal("request sent")
	}
}

func TestGroups(t *testing.T) {
	c, got := captureClient(t, 201, `{"name":"devs","groupId":"group-1"}`)
	g, err := c.AddGroupMember(context.Background(), "group-1", "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	assertAuth(t, got)
	if got.method != http.MethodPost || got.path != "/rest/api/3/group/user" {
		t.Fatalf("%s %s", got.method, got.path)
	}
	q, _ := url.ParseQuery(got.query)
	if q.Get("groupId") != "group-1" {
		t.Fatal(got.query)
	}
	if asMap(t, got.body)["accountId"] != "acct-1" || g.Name != "devs" {
		t.Fatalf("%s %+v", got.body, g)
	}

	c, got = captureClient(t, 200, "")
	if err := c.RemoveGroupMember(context.Background(), "group-1", "acct-1"); err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodDelete {
		t.Fatal(got.method)
	}
	q, _ = url.ParseQuery(got.query)
	if q.Get("groupId") != "group-1" || q.Get("accountId") != "acct-1" {
		t.Fatal(got.query)
	}

	c, got = captureClient(t, 200, `{"total":1,"isLast":false,"values":[{"accountId":"acct-1","displayName":"Ada"}]}`)
	members, err := c.GroupMembers(context.Background(), "group-1")
	if err != nil {
		t.Fatal(err)
	}
	q, _ = url.ParseQuery(got.query)
	if q.Get("groupId") != "group-1" || q.Get("startAt") != "0" || q.Get("maxResults") != "50" {
		t.Fatal(got.query)
	}
	if members.Total != 1 || members.IsLast || members.Values[0].DisplayName != "Ada" {
		t.Fatalf("%+v", members)
	}
}

func TestDescriptionLimits(t *testing.T) {
	node := map[string]any{"type": "text", "text": "x"}
	for i := 0; i < 40; i++ {
		node = map[string]any{"type": "paragraph", "content": []any{node}}
	}
	raw, err := json.Marshal(map[string]any{
		"id": "1", "key": "PROJ-1",
		"fields": map[string]any{"summary": "s", "description": node},
	})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := captureClient(t, 200, string(raw))
	_, err = c.View(context.Background(), "PROJ-1")
	if err == nil || !strings.Contains(err.Error(), "too deep") {
		t.Fatal(err)
	}
}

func fmtJSON(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, s)
}
