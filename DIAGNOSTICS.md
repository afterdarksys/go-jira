# Jira diagnostics

The standalone `jira` CLI provides the read-only diagnostics originally added to
`gdocs jira`. Existing Cloud editing commands remain available. Data Center support
applies to the diagnostic commands; existing editing commands still use Cloud v3.

## Setup

```sh
jira configure --url https://your-team.atlassian.net --email you@example.com
# Supply JIRA_API_TOKEN through your secret manager or shell environment.
jira status
jira doctor --project SEC --output plain

jira configure --deployment datacenter --url https://jira.example.com/jira
# Supply JIRA_PAT through your secret manager or shell environment.
jira doctor --issue SEC-123
```

`configure` saves non-secret TOML settings to `~/.go-jira/go-jirarc` with mode 0600.
It replaces any legacy `api_token` value with an empty value; use environment tokens
after configuring. Existing Cloud config tokens remain readable until configure runs.
`JIRA_CONFIG` selects another file; `JIRA_CONFIG=-` disables configuration loading
and cannot be used with configure. Use separate config files for separate accounts.
`status` reports settings and token availability without contacting Jira.

Cloud uses email plus API token (Basic). Data Center uses a PAT (Bearer), preserving
context paths such as `/jira`. For scoped Cloud tokens, specify `--cloud-id ID` to
route to `https://api.atlassian.com/ex/jira/ID`, or configure that gateway as the URL.
`--token-env WORK_JIRA_TOKEN` selects a custom environment variable. Tokens are never
accepted as CLI arguments.

Flags override environment variables, which override the config file. Supported
overrides: `JIRA_URL` (or legacy `JIRA_BASE_URL`), `JIRA_EMAIL`, `JIRA_DEPLOYMENT`,
`JIRA_CLOUD_ID`, `JIRA_TOKEN_ENV`, and `JIRA_OUTPUT`. `JIRA_CA_BUNDLE` adds trusted PEM
CA certificates for internal Jira servers. TLS verification cannot be disabled.

## Commands

```sh
jira doctor --project SEC --issue SEC-123
jira permissions --issue SEC-123 --permissions BROWSE_PROJECTS,EDIT_ISSUES
jira project SEC
jira issue SEC-123
jira transitions SEC-123
jira fields
jira search 'project = SEC AND statusCategory != Done' --limit 100
jira request /rest/api/3/issue/SEC-123/editmeta
jira request /rest/api/3/issue/SEC-123 --param fields=summary,status --timeout 30
```

Every diagnostic command accepts `--output json|yaml|toml|plain` (`-o` alias).
Plain output is indented JSON so nested diagnostics remain readable. The existing
root `-base-url`, `-email`, and `-o` flags also work before the diagnostic command.
`--timeout` is seconds per request, from 1 to 120 (default 20).

Doctor checks server information, current identity, permissions, and optional
project and issue access. A successful permissions response may still contain
`havePermission=false`; inspect each permission. Issue inspection includes names
and schema, and transitions includes required-field metadata. These commands do
not execute transitions or edit issues.

Search uses Cloud v3 `/search/jql` cursor pagination or Data Center v2 `/search`
offset pagination. Limits are 1–1000 issues (default 50). Reports include `truncated`
when more results exist; failures retain already collected issues and mark `partial`.
Repeated cursors, empty intermediate pages and malformed responses fail explicitly.

## Reports and exit codes

Reports contain status, elapsed milliseconds, safe correlation/rate-limit headers,
a diagnosis, and redacted JSON data. Traces contain the method, path and query keys,
not query values. HTML and other non-JSON bodies are omitted. Tokens, authorization
strings, and secret-valued JSON keys are redacted; ordinary issue content remains
visible, so review reports before sharing them.

Requests are GET-only, restricted to `/rest/api/2/` or `/rest/api/3/`, and reject
traversal, credential query keys and redirects. Each response is bounded to 5 MiB.
There are no automatic retries. Rate limits preserve Retry-After for the operator.

Exit codes: 0 success; 1 diagnostic failure; 2 invalid arguments/configuration;
4 missing credentials or HTTP 401/403; 5 connection/timeouts, 429 or common 5xx.
Doctor and search aggregate failures with authentication taking priority.

## Go API

Use `NewDiagnostics(DiagnosticConfig{...}, token, timeout, caBundle)` followed by
`Doctor`, `Inspect`, `Search`, or `Request`. Results expose `ExitCode()` and work with
`jira.Emit`. HTTP/network failures are structured results; invalid inputs return
Go errors. The existing `Client` API is unchanged.

Tests use local TLS servers and fixtures. Live Cloud/Data Center accounts have not
been exercised by these tests.
