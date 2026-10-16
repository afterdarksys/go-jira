package jira

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
)

// WorklogInput is a time-spent change.
// Set TimeSpent ("2h", "1d 30m") or TimeSpentSeconds, not both.
// AdjustEstimate is auto, leave, new, or manual.
// NewEstimate is required when AdjustEstimate is new.
// ReduceBy is required when AdjustEstimate is manual.
type WorklogInput struct {
	TimeSpent        string
	TimeSpentSeconds int
	Comment          string
	Started          *time.Time
	AdjustEstimate   string
	NewEstimate      string
	ReduceBy         string
}

// Worklogs lists time spent on an issue.
func (c *Client) Worklogs(ctx context.Context, issue string) (*WorklogPage, error) {
	path, err := issuePath(issue, "/worklog")
	if err != nil {
		return nil, err
	}
	q := url.Values{"startAt": {"0"}, "maxResults": {"100"}}
	var out WorklogPage
	if err := c.do(ctx, http.MethodGet, path, q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AddWorklog records time spent.
func (c *Client) AddWorklog(ctx context.Context, issue string, in WorklogInput) (*Worklog, error) {
	path, err := issuePath(issue, "/worklog")
	if err != nil {
		return nil, err
	}
	body, q, err := in.payload()
	if err != nil {
		return nil, err
	}
	var out Worklog
	if err := c.do(ctx, http.MethodPost, path, q, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateWorklog changes an existing time-spent entry.
func (c *Client) UpdateWorklog(ctx context.Context, issue, worklogID string, in WorklogInput) (*Worklog, error) {
	if err := validateWorklogID(worklogID); err != nil {
		return nil, err
	}
	path, err := issuePath(issue, "/worklog/"+url.PathEscape(worklogID))
	if err != nil {
		return nil, err
	}
	body, q, err := in.payload()
	if err != nil {
		return nil, err
	}
	var out Worklog
	if err := c.do(ctx, http.MethodPut, path, q, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteWorklog removes a time-spent entry.
func (c *Client) DeleteWorklog(ctx context.Context, issue, worklogID string, adjust string) error {
	if err := validateWorklogID(worklogID); err != nil {
		return err
	}
	path, err := issuePath(issue, "/worklog/"+url.PathEscape(worklogID))
	if err != nil {
		return err
	}
	q := url.Values{}
	switch adjust {
	case "", "auto":
	case "leave":
		q.Set("adjustEstimate", "leave")
	default:
		return errors.New("jira: delete adjustEstimate must be auto or leave")
	}
	return c.do(ctx, http.MethodDelete, path, q, nil, nil)
}

func validateWorklogID(id string) error {
	if !issueIDRE.MatchString(id) {
		return errors.New("jira: invalid worklog id")
	}
	return nil
}

func (in WorklogInput) payload() (map[string]any, url.Values, error) {
	if in.TimeSpent != "" && in.TimeSpentSeconds != 0 {
		return nil, nil, errors.New("jira: set time spent or seconds, not both")
	}
	if in.TimeSpent == "" && in.TimeSpentSeconds == 0 {
		return nil, nil, errors.New("jira: time spent is required")
	}
	if err := validateText("comment", in.Comment, 32<<10); err != nil {
		return nil, nil, err
	}
	body := map[string]any{}
	if in.TimeSpent != "" {
		d, err := normalizeDuration(in.TimeSpent)
		if err != nil {
			return nil, nil, err
		}
		body["timeSpent"] = d
	}
	if in.TimeSpentSeconds != 0 {
		if in.TimeSpentSeconds < 0 || in.TimeSpentSeconds > maxDurationSeconds {
			return nil, nil, errors.New("jira: invalid time spent")
		}
		body["timeSpentSeconds"] = in.TimeSpentSeconds
	}
	if in.Comment != "" {
		body["comment"] = plainADF(in.Comment)
	}
	if in.Started != nil {
		if in.Started.IsZero() {
			return nil, nil, errors.New("jira: invalid started time")
		}
		body["started"] = in.Started.UTC().Format("2006-01-02T15:04:05.000-0700")
	}
	q := url.Values{}
	switch in.AdjustEstimate {
	case "", "auto":
	case "leave":
		q.Set("adjustEstimate", "leave")
	case "new":
		d, err := normalizeDuration(in.NewEstimate)
		if err != nil {
			return nil, nil, err
		}
		q.Set("adjustEstimate", "new")
		q.Set("newEstimate", d)
	case "manual":
		d, err := normalizeDuration(in.ReduceBy)
		if err != nil {
			return nil, nil, err
		}
		q.Set("adjustEstimate", "manual")
		q.Set("reduceBy", d)
	default:
		return nil, nil, errors.New("jira: invalid adjustEstimate")
	}
	return body, q, nil
}
