package jira

import (
	"bytes"
	"encoding/json"
)

// User is a Jira account reference.
type User struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName,omitempty"`
	Active      bool   `json:"active,omitempty"`
}

// IDName is a Jira object that has an id and a display name.
type IDName struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// Project is the issue's project.
type Project struct {
	ID   string `json:"id,omitempty"`
	Key  string `json:"key,omitempty"`
	Name string `json:"name,omitempty"`
}

// TimeTracking is the issue's original estimate, remaining estimate, and time spent.
type TimeTracking struct {
	OriginalEstimate         string `json:"originalEstimate,omitempty"`
	RemainingEstimate        string `json:"remainingEstimate,omitempty"`
	TimeSpent                string `json:"timeSpent,omitempty"`
	OriginalEstimateSeconds  int64  `json:"originalEstimateSeconds,omitempty"`
	RemainingEstimateSeconds int64  `json:"remainingEstimateSeconds,omitempty"`
	TimeSpentSeconds         int64  `json:"timeSpentSeconds,omitempty"`
}

// WatchInfo is the watch summary stored on an issue.
type WatchInfo struct {
	WatchCount int  `json:"watchCount"`
	IsWatching bool `json:"isWatching"`
}

// IssueFields are the issue fields this client reads directly.
// The full payload, including custom fields, is kept for Emit.
type IssueFields struct {
	Summary      string        `json:"summary,omitempty"`
	Description  string        `json:"-"`
	Status       *IDName       `json:"status,omitempty"`
	Assignee     *User         `json:"assignee,omitempty"`
	Reporter     *User         `json:"reporter,omitempty"`
	Labels       []string      `json:"labels,omitempty"`
	DueDate      string        `json:"duedate,omitempty"`
	TimeTracking *TimeTracking `json:"timetracking,omitempty"`
	Priority     *IDName       `json:"priority,omitempty"`
	IssueType    *IDName       `json:"issuetype,omitempty"`
	Project      *Project      `json:"project,omitempty"`
	Created      string        `json:"created,omitempty"`
	Updated      string        `json:"updated,omitempty"`
	Watches      *WatchInfo    `json:"watches,omitempty"`
}

// UnmarshalJSON accepts both wiki text and Atlassian document descriptions.
func (f *IssueFields) UnmarshalJSON(data []byte) error {
	var aux struct {
		Summary      string          `json:"summary"`
		Description  json.RawMessage `json:"description"`
		Status       *IDName         `json:"status"`
		Assignee     *User           `json:"assignee"`
		Reporter     *User           `json:"reporter"`
		Labels       []string        `json:"labels"`
		DueDate      string          `json:"duedate"`
		TimeTracking *TimeTracking   `json:"timetracking"`
		Priority     *IDName         `json:"priority"`
		IssueType    *IDName         `json:"issuetype"`
		Project      *Project        `json:"project"`
		Created      string          `json:"created"`
		Updated      string          `json:"updated"`
		Watches      *WatchInfo      `json:"watches"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	text, err := adfPlain(aux.Description)
	if err != nil {
		return err
	}
	f.Summary = aux.Summary
	f.Description = text
	f.Status = aux.Status
	f.Assignee = aux.Assignee
	f.Reporter = aux.Reporter
	f.Labels = aux.Labels
	f.DueDate = aux.DueDate
	f.TimeTracking = aux.TimeTracking
	f.Priority = aux.Priority
	f.IssueType = aux.IssueType
	f.Project = aux.Project
	f.Created = aux.Created
	f.Updated = aux.Updated
	f.Watches = aux.Watches
	return nil
}

// Issue is a Jira issue. RawJSON is the response body, used by Emit.
type Issue struct {
	ID     string      `json:"id,omitempty"`
	Key    string      `json:"key,omitempty"`
	Self   string      `json:"self,omitempty"`
	Fields IssueFields `json:"fields"`
	raw    json.RawMessage
}

// UnmarshalJSON keeps the original body so Emit can write every field.
func (i *Issue) UnmarshalJSON(data []byte) error {
	type issue Issue
	var a issue
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*i = Issue(a)
	i.raw = bytes.Clone(data)
	return nil
}

// RawJSON returns the original issue payload.
func (i Issue) RawJSON() []byte { return i.raw }

// Watchers is the watcher list for an issue.
type Watchers struct {
	WatchCount int    `json:"watchCount"`
	IsWatching bool   `json:"isWatching"`
	Watchers   []User `json:"watchers"`
	raw        json.RawMessage
}

// UnmarshalJSON keeps the original body.
func (w *Watchers) UnmarshalJSON(data []byte) error {
	type watchers Watchers
	var a watchers
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*w = Watchers(a)
	w.raw = bytes.Clone(data)
	return nil
}

// RawJSON returns the original watcher payload.
func (w Watchers) RawJSON() []byte { return w.raw }

// Worklog is one time-spent entry.
type Worklog struct {
	ID               string `json:"id,omitempty"`
	TimeSpent        string `json:"timeSpent,omitempty"`
	TimeSpentSeconds int64  `json:"timeSpentSeconds,omitempty"`
	Started          string `json:"started,omitempty"`
	raw              json.RawMessage
}

// UnmarshalJSON keeps the original body.
func (w *Worklog) UnmarshalJSON(data []byte) error {
	type worklog Worklog
	var a worklog
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*w = Worklog(a)
	w.raw = bytes.Clone(data)
	return nil
}

// RawJSON returns the original worklog payload.
func (w Worklog) RawJSON() []byte { return w.raw }

// WorklogPage is a page of worklogs.
type WorklogPage struct {
	StartAt    int       `json:"startAt"`
	MaxResults int       `json:"maxResults"`
	Total      int       `json:"total"`
	Worklogs   []Worklog `json:"worklogs"`
	raw        json.RawMessage
}

// UnmarshalJSON keeps the original body.
func (p *WorklogPage) UnmarshalJSON(data []byte) error {
	type page WorklogPage
	var a page
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*p = WorklogPage(a)
	p.raw = bytes.Clone(data)
	return nil
}

// RawJSON returns the original worklog page.
func (p WorklogPage) RawJSON() []byte { return p.raw }

// Group is a Jira user group.
type Group struct {
	Name    string `json:"name,omitempty"`
	GroupID string `json:"groupId,omitempty"`
	Self    string `json:"self,omitempty"`
	raw     json.RawMessage
}

// UnmarshalJSON keeps the original body.
func (g *Group) UnmarshalJSON(data []byte) error {
	type group Group
	var a group
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*g = Group(a)
	g.raw = bytes.Clone(data)
	return nil
}

// RawJSON returns the original group payload.
func (g Group) RawJSON() []byte { return g.raw }

// GroupMembers is one page of a group's members.
type GroupMembers struct {
	Total  int    `json:"total"`
	IsLast bool   `json:"isLast"`
	Values []User `json:"values"`
	raw    json.RawMessage
}

// UnmarshalJSON keeps the original body.
func (g *GroupMembers) UnmarshalJSON(data []byte) error {
	type members GroupMembers
	var a members
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*g = GroupMembers(a)
	g.raw = bytes.Clone(data)
	return nil
}

// RawJSON returns the original member page.
func (g GroupMembers) RawJSON() []byte { return g.raw }

// Result is the body emitted for operations that return no Jira payload.
type Result struct {
	OK        bool   `json:"ok"`
	Action    string `json:"action"`
	Issue     string `json:"issue,omitempty"`
	GroupID   string `json:"groupId,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	WorklogID string `json:"worklogId,omitempty"`
}
