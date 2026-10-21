package jira

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const DefaultPermissions = "BROWSE_PROJECTS,CREATE_ISSUES,EDIT_ISSUES,TRANSITION_ISSUES"

func permissionQuery(project, issue, permissions string) (url.Values, error) {
	if permissions == "" {
		permissions = DefaultPermissions
	}
	for _, p := range strings.Split(permissions, ",") {
		if !regexp.MustCompile(`^[A-Z][A-Z_]*$`).MatchString(p) {
			return nil, errors.New("invalid permission keys")
		}
	}
	q := url.Values{"permissions": {permissions}}
	for k, v := range map[string]string{"projectKey": project, "issueKey": issue} {
		if v != "" {
			if !diagID.MatchString(v) {
				return nil, errors.New("invalid project/issue identifier")
			}
			q.Set(k, v)
		}
	}
	return q, nil
}

// Inspect retrieves permissions, fields, project, issue, or transitions metadata.
func (d *Diagnostics) Inspect(ctx context.Context, action, target, project, issue, permissions string) (DiagnosticResult, error) {
	q := url.Values{}
	path := ""
	switch action {
	case "permissions":
		var err error
		q, err = permissionQuery(project, issue, permissions)
		if err != nil {
			return nil, err
		}
		path = "/mypermissions"
	case "fields":
		path = "/field"
	case "project", "issue", "transitions":
		if !diagID.MatchString(target) {
			return nil, errors.New("invalid resource identifier")
		}
		path = "/" + action + "/" + target
		if action == "issue" {
			q.Set("fields", "summary,status,project,issuetype,assignee")
			q.Set("expand", "names,schema")
		}
		if action == "transitions" {
			path = "/issue/" + target + "/transitions"
			q.Set("expand", "transitions.fields")
		}
	default:
		return nil, errors.New("unknown diagnostic resource")
	}
	return d.Request(ctx, d.api+path, q)
}

// Doctor checks server, identity, permissions, and optional project/issue access.
func (d *Diagnostics) Doctor(ctx context.Context, project, issue string) (DiagnosticResult, error) {
	q, err := permissionQuery(project, issue, DefaultPermissions)
	if err != nil {
		return nil, err
	}
	requests := []struct {
		name, path string
		q          url.Values
	}{{"server", "/serverInfo", nil}, {"identity", "/myself", nil}, {"permissions", "/mypermissions", q}}
	if project != "" {
		requests = append(requests, struct {
			name, path string
			q          url.Values
		}{"project", "/project/" + project, nil})
	}
	if issue != "" {
		requests = append(requests, struct {
			name, path string
			q          url.Values
		}{"issue", "/issue/" + issue, url.Values{"fields": {"summary,status,project"}}})
	}
	checks := []DiagnosticResult{}
	ok := true
	for _, req := range requests {
		row, e := d.Request(ctx, d.api+req.path, req.q)
		if e != nil {
			return nil, e
		}
		row["check"] = req.name
		checks = append(checks, row)
		ok = ok && row["ok"] == true
	}
	return DiagnosticResult{"ok": ok, "deployment": d.deployment, "checks": checks, "note": "Successful permission requests may contain havePermission=false; inspect each value."}, nil
}

// Search pages Cloud cursor or Data Center offset results, bounded by limit.
func (d *Diagnostics) Search(ctx context.Context, jql string, limit int) (DiagnosticResult, error) {
	if strings.TrimSpace(jql) == "" || len(jql) > 10000 || limit < 1 || limit > 1000 {
		return nil, errors.New("JQL must contain 1–10000 bytes and limit must be 1–1000")
	}
	cloud := d.deployment == "cloud"
	path := d.api + "/search"
	if cloud {
		path += "/jql"
	}
	issues := []any{}
	checks := []DiagnosticResult{}
	cursor := ""
	offset := 0
	seen := map[string]bool{}
	more := false
	failure := func(message string) DiagnosticResult {
		return DiagnosticResult{"ok": false, "issues": issues, "checks": checks, "partial": len(issues) > 0, "diagnosis": message}
	}
	for len(issues) < limit {
		size := min(100, limit-len(issues))
		q := url.Values{"jql": {jql}, "maxResults": {strconv.Itoa(size)}, "fields": {"summary,status,project,issuetype"}}
		if cloud && cursor != "" {
			q.Set("nextPageToken", cursor)
		}
		if !cloud {
			q.Set("startAt", strconv.Itoa(offset))
		}
		row, err := d.Request(ctx, path, q)
		if err != nil {
			return nil, err
		}
		data := row["data"]
		delete(row, "data")
		checks = append(checks, row)
		if row["ok"] != true {
			r := failure("Search request failed.")
			r["error"] = data
			return r, nil
		}
		obj, ok := data.(map[string]any)
		if !ok {
			return failure("Unexpected search response shape."), nil
		}
		batch, ok := obj["issues"].([]any)
		if !ok {
			return failure("Unexpected search issues shape."), nil
		}
		issues = append(issues, batch[:min(len(batch), limit-len(issues))]...)
		if cloud {
			var valid bool
			cursor, valid = obj["nextPageToken"].(string)
			if obj["nextPageToken"] != nil && !valid {
				return failure("Invalid search cursor."), nil
			}
			more = cursor != "" && obj["isLast"] != true
			if more && seen[cursor] {
				return failure("Repeated search cursor refused."), nil
			}
			seen[cursor] = true
		} else {
			offset += len(batch)
			total, ok := obj["total"].(float64)
			if !ok || total < 0 || total != float64(int(total)) {
				return failure("Invalid search total."), nil
			}
			more = float64(offset) < total
		}
		if more && len(batch) == 0 {
			return failure("Empty page with more results refused."), nil
		}
		if !more {
			break
		}
	}
	return DiagnosticResult{"ok": true, "issues": issues, "count": len(issues), "truncated": more, "checks": checks}, nil
}
