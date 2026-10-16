package jira

import (
	"context"
	"errors"
	"net/http"
)

// View returns one issue. Pass an issue key such as PROJ-1, or a numeric id.
func (c *Client) View(ctx context.Context, issue string) (*Issue, error) {
	path, err := issuePath(issue, "")
	if err != nil {
		return nil, err
	}
	var out Issue
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Update changes issue fields. Set only the values that should change.
// Labels replaces the label set when SetLabels is true.
// AddLabels and RemoveLabels edit the set. They cannot be combined with SetLabels.
// ClearDue clears the due date. Due sets it to YYYY-MM-DD.
// TimeSpent adds a worklog in the same request.
type Update struct {
	Summary           *string
	Description       *string
	SetLabels         bool
	Labels            []string
	AddLabels         []string
	RemoveLabels      []string
	Due               *string
	ClearDue          bool
	OriginalEstimate  *string
	RemainingEstimate *string
	TimeSpent         string
	TimeComment       string
}

// Update edits an issue.
func (c *Client) Update(ctx context.Context, issue string, u Update) error {
	path, err := issuePath(issue, "")
	if err != nil {
		return err
	}
	body, err := u.payload()
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPut, path, nil, body, nil)
}

func (u Update) payload() (map[string]any, error) {
	if u.SetLabels && (len(u.AddLabels) > 0 || len(u.RemoveLabels) > 0) {
		return nil, errors.New("jira: set labels or add and remove them, not both")
	}
	if u.Due != nil && u.ClearDue {
		return nil, errors.New("jira: set a due date or clear it, not both")
	}
	fields := map[string]any{}
	update := map[string]any{}

	if u.Summary != nil {
		if err := validateSummary(*u.Summary); err != nil {
			return nil, err
		}
		fields["summary"] = *u.Summary
	}
	if u.Description != nil {
		if err := validateText("description", *u.Description, 32<<10); err != nil {
			return nil, err
		}
		if *u.Description == "" {
			fields["description"] = nil
		} else {
			fields["description"] = plainADF(*u.Description)
		}
	}
	if u.SetLabels {
		if u.Labels == nil {
			u.Labels = []string{}
		}
		if err := validateLabels(u.Labels); err != nil {
			return nil, err
		}
		fields["labels"] = u.Labels
	}
	if err := validateLabels(u.AddLabels); err != nil {
		return nil, err
	}
	if err := validateLabels(u.RemoveLabels); err != nil {
		return nil, err
	}
	var labelOps []map[string]string
	for _, s := range u.AddLabels {
		labelOps = append(labelOps, map[string]string{"add": s})
	}
	for _, s := range u.RemoveLabels {
		labelOps = append(labelOps, map[string]string{"remove": s})
	}
	if len(labelOps) > 0 {
		update["labels"] = labelOps
	}
	if u.ClearDue {
		fields["duedate"] = nil
	}
	if u.Due != nil {
		if err := validateDue(*u.Due); err != nil {
			return nil, err
		}
		fields["duedate"] = *u.Due
	}
	track := map[string]any{}
	if u.OriginalEstimate != nil {
		d, err := normalizeDuration(*u.OriginalEstimate)
		if err != nil {
			return nil, err
		}
		track["originalEstimate"] = d
	}
	if u.RemainingEstimate != nil {
		d, err := normalizeDuration(*u.RemainingEstimate)
		if err != nil {
			return nil, err
		}
		track["remainingEstimate"] = d
	}
	if len(track) > 0 {
		fields["timetracking"] = track
	}
	if u.TimeSpent != "" {
		spent, err := normalizeDuration(u.TimeSpent)
		if err != nil {
			return nil, err
		}
		if err := validateText("comment", u.TimeComment, 32<<10); err != nil {
			return nil, err
		}
		entry := map[string]any{"timeSpent": spent}
		if u.TimeComment != "" {
			entry["comment"] = plainADF(u.TimeComment)
		}
		update["worklog"] = []any{map[string]any{"add": entry}}
	} else if u.TimeComment != "" {
		return nil, errors.New("jira: time comment requires time spent")
	}

	if len(fields) == 0 && len(update) == 0 {
		return nil, errors.New("jira: empty update")
	}
	body := map[string]any{}
	if len(fields) > 0 {
		body["fields"] = fields
	}
	if len(update) > 0 {
		body["update"] = update
	}
	return body, nil
}

// Assign sets the assignee. Call it again with another account to reassign.
func (c *Client) Assign(ctx context.Context, issue, accountID string) error {
	path, err := issuePath(issue, "/assignee")
	if err != nil {
		return err
	}
	if err := validateObjectID("account id", accountID); err != nil {
		return err
	}
	body := map[string]any{"accountId": accountID}
	return c.do(ctx, http.MethodPut, path, nil, body, nil)
}

// Unassign clears the assignee.
func (c *Client) Unassign(ctx context.Context, issue string) error {
	path, err := issuePath(issue, "/assignee")
	if err != nil {
		return err
	}
	body := map[string]any{"accountId": nil}
	return c.do(ctx, http.MethodPut, path, nil, body, nil)
}
