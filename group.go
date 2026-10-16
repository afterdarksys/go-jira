package jira

import (
	"context"
	"net/http"
	"net/url"
)

// AddGroupMember adds an account to a Jira group.
func (c *Client) AddGroupMember(ctx context.Context, groupID, accountID string) (*Group, error) {
	if err := validateObjectID("group id", groupID); err != nil {
		return nil, err
	}
	if err := validateObjectID("account id", accountID); err != nil {
		return nil, err
	}
	q := url.Values{"groupId": {groupID}}
	body := map[string]string{"accountId": accountID}
	var out Group
	if err := c.do(ctx, http.MethodPost, "/rest/api/3/group/user", q, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoveGroupMember removes an account from a Jira group.
func (c *Client) RemoveGroupMember(ctx context.Context, groupID, accountID string) error {
	if err := validateObjectID("group id", groupID); err != nil {
		return err
	}
	if err := validateObjectID("account id", accountID); err != nil {
		return err
	}
	q := url.Values{
		"groupId":   {groupID},
		"accountId": {accountID},
	}
	return c.do(ctx, http.MethodDelete, "/rest/api/3/group/user", q, nil, nil)
}

// GroupMembers returns the first page of a group's members (up to 50).
// IsLast is false when the group has more members than this page.
func (c *Client) GroupMembers(ctx context.Context, groupID string) (*GroupMembers, error) {
	if err := validateObjectID("group id", groupID); err != nil {
		return nil, err
	}
	q := url.Values{
		"groupId":    {groupID},
		"startAt":    {"0"},
		"maxResults": {"50"},
	}
	var out GroupMembers
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/group/member", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
