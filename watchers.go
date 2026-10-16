package jira

import (
	"context"
	"net/http"
	"net/url"
)

// Watchers returns the users watching an issue.
func (c *Client) Watchers(ctx context.Context, issue string) (*Watchers, error) {
	path, err := issuePath(issue, "/watchers")
	if err != nil {
		return nil, err
	}
	var out Watchers
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AddWatcher makes accountID watch the issue.
// An empty accountID watches as the authenticated user.
func (c *Client) AddWatcher(ctx context.Context, issue, accountID string) error {
	path, err := issuePath(issue, "/watchers")
	if err != nil {
		return err
	}
	var body any
	if accountID != "" {
		if err := validateObjectID("account id", accountID); err != nil {
			return err
		}
		body = accountID
	}
	return c.do(ctx, http.MethodPost, path, nil, body, nil)
}

// RemoveWatcher stops accountID from watching the issue.
func (c *Client) RemoveWatcher(ctx context.Context, issue, accountID string) error {
	path, err := issuePath(issue, "/watchers")
	if err != nil {
		return err
	}
	if err := validateObjectID("account id", accountID); err != nil {
		return err
	}
	q := url.Values{"accountId": {accountID}}
	return c.do(ctx, http.MethodDelete, path, q, nil, nil)
}
