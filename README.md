# go-jira

Go client and `jira` command for Jira Cloud REST API v3.

It can view and update issues, assign and unassign them, add and remove watchers, change labels, due dates, and time spent, and add or remove group members. Output is JSON, YAML, or TOML.

## Install

```bash
./build.sh build
./build.sh install
```

`install` copies the binary to `/usr/local/bin/jira`. It also creates `~/.go-jira/go-jirarc` when that file is missing. The directory is mode `0700` and the file is mode `0600`.

From this repository:

```bash
go install github.com/afterdarksys/go-jira/cmd/jira@latest
```

`./build.sh run view PROJ-1` builds the local binary and runs it.

## Configure

`~/.go-jira/go-jirarc` is TOML:

```toml
base_url = "https://your-site.atlassian.net"
email = "you@example.com"
output = "json"
api_token = ""
```

Create the API token at https://id.atlassian.com/manage-profile/security/api-tokens.

Flags override environment variables. Environment variables override the file.

| Setting | Flag | Environment | File |
| --- | --- | --- | --- |
| Site URL | `-base-url` | `JIRA_BASE_URL` | `base_url` |
| Email | `-email` | `JIRA_EMAIL` | `email` |
| Output | `-o` | `JIRA_OUTPUT` | `output` |
| API token | none | `JIRA_API_TOKEN` | `api_token` |

The token is never a command-line flag. `JIRA_CONFIG` selects another config file. `JIRA_CONFIG=-` skips the file.

The loader rejects a symlink, a file that is not owner-only, a directory that is writable by the group or others, and a file larger than 16 KiB. A broken file returns `invalid go-jirarc` and does not echo its contents.

Connections require HTTPS and TLS 1.3. Redirects are refused.

## Commands

Global flags come before the command. Issue flags can come before or after the issue key.

```bash
jira view PROJ-1
jira -o yaml view PROJ-1
jira update PROJ-1 -summary "Renamed" -labels a,b -due 2026-10-16
jira assign PROJ-1 -account ACCOUNT_ID
jira unassign PROJ-1
jira watch add PROJ-1 -account ACCOUNT_ID
jira watch remove PROJ-1 -account ACCOUNT_ID
jira time add PROJ-1 -spent 2h
jira group add -group GROUP_ID -account ACCOUNT_ID
```

`-o` accepts `json`, `yaml` (or `yml`), and `toml`.

```text
view ISSUE
update ISSUE [-summary S] [-description TEXT] [-labels a,b]
             [-add-label a] [-remove-label b] [-due YYYY-MM-DD] [-clear-due]
             [-original-estimate 1d] [-remaining-estimate 4h]
             [-time-spent 2h] [-time-comment TEXT]
assign ISSUE -account ACCOUNT
unassign ISSUE
watch list|add|remove ISSUE [-account ACCOUNT]
time list ISSUE
time add ISSUE (-spent 2h | -seconds N) [-comment TEXT] [-started RFC3339]
              [-adjust auto|leave|new|manual] [-new-estimate 1h] [-reduce-by 30m]
time update ISSUE -id ID (-spent 2h | -seconds N)
time delete ISSUE -id ID [-adjust auto|leave]
group members|add|remove -group GROUP [-account ACCOUNT]
```

Descriptions and worklog comments are sent as Atlassian Document Format. Time spent is a worklog. Groups are Jira Cloud group membership by group id.

Exit `0` on success, `2` on usage or configuration errors, and `1` on an API or client error.

## Library

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/afterdarksys/go-jira"
)

func main() {
	c, err := jira.New(os.Getenv("JIRA_BASE_URL"), os.Getenv("JIRA_EMAIL"), os.Getenv("JIRA_API_TOKEN"))
	if err != nil {
		panic(err)
	}
	issue, err := c.View(context.Background(), "PROJ-1")
	if err != nil {
		panic(err)
	}
	if err := jira.Emit(os.Stdout, "yaml", issue); err != nil {
		panic(err)
	}
	fmt.Println()
}
```

`jira.Emit` writes JSON, YAML, or TOML. Issue responses keep the original JSON body, so custom fields are included.
