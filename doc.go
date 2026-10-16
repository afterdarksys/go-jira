// Package jira is a client for Jira Cloud.
//
// Threats: protects the API token from leaking through error text, fmt
// output, and cross-host redirects. Requires TLS 1.3 and rejects disabled
// certificate checks. Does not rotate tokens, implement OAuth, or stop a
// caller from printing a token they already hold.
//
// Authenticate with an Atlassian account email and an API token
// (https://id.atlassian.com/manage-profile/security/api-tokens).
// The site URL looks like https://your-domain.atlassian.net.
package jira
