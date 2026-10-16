package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/afterdarksys/go-jira"
)

type newClient func(baseURL, email, token string) (*jira.Client, error)

func main() {
	env := map[string]string{
		"JIRA_API_TOKEN": os.Getenv("JIRA_API_TOKEN"),
		"JIRA_EMAIL":     os.Getenv("JIRA_EMAIL"),
		"JIRA_BASE_URL":  os.Getenv("JIRA_BASE_URL"),
		"JIRA_OUTPUT":    os.Getenv("JIRA_OUTPUT"),
		"HOME":           os.Getenv("HOME"),
	}
	if p, ok := os.LookupEnv("JIRA_CONFIG"); ok {
		env["JIRA_CONFIG"] = p
	}
	os.Exit(run(os.Args[1:], env, os.Stdout, os.Stderr, func(base, email, token string) (*jira.Client, error) {
		return jira.New(base, email, token)
	}))
}

func run(args []string, env map[string]string, stdout, stderr io.Writer, newFn newClient) int {
	fs := flag.NewFlagSet("jira", flag.ContinueOnError)
	fs.SetOutput(stderr)
	base := fs.String("base-url", env["JIRA_BASE_URL"], "Jira Cloud site URL")
	email := fs.String("email", env["JIRA_EMAIL"], "Atlassian account email")
	format := fs.String("o", env["JIRA_OUTPUT"], "output format: json, yaml, or toml")
	fs.Usage = func() { fmt.Fprint(stderr, usageText) }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	rest := fs.Args()
	if *format != "" && !validFormat(*format) {
		fmt.Fprintf(stderr, "unknown format %q\n", *format)
		return 2
	}
	if len(rest) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	cmd := rest[0]
	cmdArgs := rest[1:]
	if cmd == "help" {
		fmt.Fprint(stdout, usageText)
		return 0
	}
	if !known(cmd) {
		fmt.Fprintf(stderr, "unknown command %q\n", cmd)
		fmt.Fprint(stderr, usageText)
		return 2
	}

	cfg, err := loadConfig(env)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	seen := visited(fs)
	if !seen["base-url"] && env["JIRA_BASE_URL"] == "" {
		*base = cfg.BaseURL
	}
	if !seen["email"] && env["JIRA_EMAIL"] == "" {
		*email = cfg.Email
	}
	if !seen["o"] && env["JIRA_OUTPUT"] == "" && cfg.Output != "" {
		*format = cfg.Output
	}
	token := env["JIRA_API_TOKEN"]
	if token == "" {
		token = cfg.APIToken
	}
	if *format == "" {
		*format = "json"
	}
	if !validFormat(*format) {
		fmt.Fprintf(stderr, "unknown format %q\n", *format)
		return 2
	}

	dial := func() (*jira.Client, int, bool) {
		return connect(*base, *email, token, stderr, newFn)
	}

	ctx := context.Background()
	switch cmd {
	case "view":
		return cmdView(ctx, dial, cmdArgs, *format, stdout, stderr)
	case "update":
		return cmdUpdate(ctx, dial, cmdArgs, *format, stdout, stderr)
	case "assign":
		return cmdAssign(ctx, dial, cmdArgs, *format, stdout, stderr)
	case "unassign":
		return cmdUnassign(ctx, dial, cmdArgs, *format, stdout, stderr)
	case "watch":
		return cmdWatch(ctx, dial, cmdArgs, *format, stdout, stderr)
	case "time":
		return cmdTime(ctx, dial, cmdArgs, *format, stdout, stderr)
	case "group":
		return cmdGroup(ctx, dial, cmdArgs, *format, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", cmd)
		fmt.Fprint(stderr, usageText)
		return 2
	}
}

type dialFunc func() (*jira.Client, int, bool)

func useClient(dial dialFunc) (*jira.Client, int) {
	c, code, ok := dial()
	if !ok {
		return nil, code
	}
	return c, 0
}

func validFormat(s string) bool {
	switch strings.ToLower(s) {
	case "json", "yaml", "yml", "toml":
		return true
	default:
		return false
	}
}

func known(cmd string) bool {
	switch cmd {
	case "help", "view", "update", "assign", "unassign", "watch", "time", "group":
		return true
	default:
		return false
	}
}

func connect(base, email, token string, stderr io.Writer, newFn newClient) (*jira.Client, int, bool) {
	if base == "" || email == "" {
		fmt.Fprintln(stderr, "set -base-url and -email, JIRA_BASE_URL and JIRA_EMAIL, or base_url and email in ~/.go-jira/go-jirarc")
		return nil, 2, false
	}
	if token == "" {
		fmt.Fprintln(stderr, "JIRA_API_TOKEN is not set")
		return nil, 2, false
	}
	c, err := newFn(base, email, token)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return nil, 1, false
	}
	return c, 0, true
}

func cmdView(ctx context.Context, dial dialFunc, args []string, format string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if code, ok := parseSet(fs, args, stderr); !ok {
		return code
	}
	issue, ok := oneArg(fs, stderr, "view")
	if !ok {
		return 2
	}
	c, code := useClient(dial)
	if c == nil {
		return code
	}
	got, err := c.View(ctx, issue)
	return write(stdout, stderr, format, got, err)
}

func cmdUpdate(ctx context.Context, dial dialFunc, args []string, format string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	summary := fs.String("summary", "", "issue summary")
	description := fs.String("description", "", "plain-text description")
	labels := fs.String("labels", "", "replace labels, comma-separated")
	add := fs.String("add-label", "", "labels to add, comma-separated")
	remove := fs.String("remove-label", "", "labels to remove, comma-separated")
	due := fs.String("due", "", "due date YYYY-MM-DD")
	clearDue := fs.Bool("clear-due", false, "clear the due date")
	original := fs.String("original-estimate", "", "original estimate, such as 1d or 4h")
	remaining := fs.String("remaining-estimate", "", "remaining estimate")
	spent := fs.String("time-spent", "", "time spent to log, such as 2h")
	comment := fs.String("time-comment", "", "comment for the time spent")
	if code, ok := parseSet(fs, args, stderr); !ok {
		return code
	}
	issue, ok := oneArg(fs, stderr, "update")
	if !ok {
		return 2
	}
	seen := visited(fs)
	var u jira.Update
	if seen["summary"] {
		u.Summary = summary
	}
	if seen["description"] {
		u.Description = description
	}
	if seen["labels"] {
		list, err := splitList(*labels)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		u.SetLabels = true
		u.Labels = list
	}
	if seen["add-label"] {
		list, err := splitList(*add)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		u.AddLabels = list
	}
	if seen["remove-label"] {
		list, err := splitList(*remove)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		u.RemoveLabels = list
	}
	if seen["due"] {
		u.Due = due
	}
	u.ClearDue = *clearDue
	if seen["original-estimate"] {
		u.OriginalEstimate = original
	}
	if seen["remaining-estimate"] {
		u.RemainingEstimate = remaining
	}
	if seen["time-spent"] {
		u.TimeSpent = *spent
	}
	if seen["time-comment"] {
		u.TimeComment = *comment
	}
	c, code := useClient(dial)
	if c == nil {
		return code
	}
	if err := c.Update(ctx, issue, u); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return write(stdout, stderr, format, jira.Result{OK: true, Action: "update", Issue: issue}, nil)
}

func cmdAssign(ctx context.Context, dial dialFunc, args []string, format string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("assign", flag.ContinueOnError)
	fs.SetOutput(stderr)
	account := fs.String("account", "", "assignee account id")
	if code, ok := parseSet(fs, args, stderr); !ok {
		return code
	}
	issue, ok := oneArg(fs, stderr, "assign")
	if !ok {
		return 2
	}
	if *account == "" {
		fmt.Fprintln(stderr, "assign requires -account")
		return 2
	}
	c, code := useClient(dial)
	if c == nil {
		return code
	}
	if err := c.Assign(ctx, issue, *account); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return write(stdout, stderr, format, jira.Result{OK: true, Action: "assign", Issue: issue, AccountID: *account}, nil)
}

func cmdUnassign(ctx context.Context, dial dialFunc, args []string, format string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("unassign", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if code, ok := parseSet(fs, args, stderr); !ok {
		return code
	}
	issue, ok := oneArg(fs, stderr, "unassign")
	if !ok {
		return 2
	}
	c, code := useClient(dial)
	if c == nil {
		return code
	}
	if err := c.Unassign(ctx, issue); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return write(stdout, stderr, format, jira.Result{OK: true, Action: "unassign", Issue: issue}, nil)
}

func cmdWatch(ctx context.Context, dial dialFunc, args []string, format string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "watch requires list, add, or remove")
		return 2
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("watch "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	account := fs.String("account", "", "watcher account id")
	if code, ok := parseSet(fs, rest, stderr); !ok {
		return code
	}
	issue, ok := oneArg(fs, stderr, "watch "+sub)
	if !ok {
		return 2
	}
	if sub == "remove" && *account == "" {
		fmt.Fprintln(stderr, "watch remove requires -account")
		return 2
	}
	if sub != "list" && sub != "add" && sub != "remove" {
		fmt.Fprintf(stderr, "unknown watch command %q\n", sub)
		return 2
	}
	c, code := useClient(dial)
	if c == nil {
		return code
	}
	switch sub {
	case "list":
		got, err := c.Watchers(ctx, issue)
		return write(stdout, stderr, format, got, err)
	case "add":
		if err := c.AddWatcher(ctx, issue, *account); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		res := jira.Result{OK: true, Action: "watch-add", Issue: issue, AccountID: *account}
		return write(stdout, stderr, format, res, nil)
	case "remove":
		if err := c.RemoveWatcher(ctx, issue, *account); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		res := jira.Result{OK: true, Action: "watch-remove", Issue: issue, AccountID: *account}
		return write(stdout, stderr, format, res, nil)
	default:
		fmt.Fprintf(stderr, "unknown watch command %q\n", sub)
		return 2
	}
}

func cmdTime(ctx context.Context, dial dialFunc, args []string, format string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "time requires list, add, update, or delete")
		return 2
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("time "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "worklog id")
	spent := fs.String("spent", "", "time spent, such as 2h or 1d 30m")
	seconds := fs.Int("seconds", 0, "time spent in seconds")
	comment := fs.String("comment", "", "worklog comment")
	started := fs.String("started", "", "start time, RFC3339")
	adjust := fs.String("adjust", "auto", "auto, leave, new, or manual")
	newEst := fs.String("new-estimate", "", "remaining estimate when -adjust new")
	reduce := fs.String("reduce-by", "", "amount removed when -adjust manual")
	if code, ok := parseSet(fs, rest, stderr); !ok {
		return code
	}
	issue, ok := oneArg(fs, stderr, "time "+sub)
	if !ok {
		return 2
	}
	switch sub {
	case "list", "add", "update", "delete":
	default:
		fmt.Fprintf(stderr, "unknown time command %q\n", sub)
		return 2
	}
	if (sub == "update" || sub == "delete") && *id == "" {
		fmt.Fprintf(stderr, "time %s requires -id\n", sub)
		return 2
	}
	var in jira.WorklogInput
	if sub == "add" || sub == "update" {
		var err error
		in, err = worklogInput(*spent, *seconds, *comment, *started, *adjust, *newEst, *reduce, visited(fs))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	c, code := useClient(dial)
	if c == nil {
		return code
	}
	switch sub {
	case "list":
		got, err := c.Worklogs(ctx, issue)
		return write(stdout, stderr, format, got, err)
	case "add", "update":
		var got *jira.Worklog
		var err error
		if sub == "add" {
			got, err = c.AddWorklog(ctx, issue, in)
		} else {
			got, err = c.UpdateWorklog(ctx, issue, *id, in)
		}
		return write(stdout, stderr, format, got, err)
	case "delete":
		if err := c.DeleteWorklog(ctx, issue, *id, *adjust); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		res := jira.Result{OK: true, Action: "time-delete", Issue: issue, WorklogID: *id}
		return write(stdout, stderr, format, res, nil)
	default:
		fmt.Fprintf(stderr, "unknown time command %q\n", sub)
		return 2
	}
}

func worklogInput(spent string, seconds int, comment, started, adjust, newEst, reduce string, seen map[string]bool) (jira.WorklogInput, error) {
	in := jira.WorklogInput{
		Comment:        comment,
		AdjustEstimate: adjust,
		NewEstimate:    newEst,
		ReduceBy:       reduce,
	}
	if seen["spent"] {
		in.TimeSpent = spent
	}
	if seen["seconds"] {
		in.TimeSpentSeconds = seconds
	}
	if seen["started"] {
		t, err := time.Parse(time.RFC3339, started)
		if err != nil {
			return in, errors.New("started must be RFC3339")
		}
		in.Started = &t
	}
	return in, nil
}

func cmdGroup(ctx context.Context, dial dialFunc, args []string, format string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "group requires members, add, or remove")
		return 2
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("group "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	groupID := fs.String("group", "", "group id")
	account := fs.String("account", "", "account id")
	if code, ok := parseSet(fs, rest, stderr); !ok {
		return code
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "group "+sub+" does not take positional arguments")
		return 2
	}
	switch sub {
	case "members", "add", "remove":
	default:
		fmt.Fprintf(stderr, "unknown group command %q\n", sub)
		return 2
	}
	if *groupID == "" {
		fmt.Fprintln(stderr, "group "+sub+" requires -group")
		return 2
	}
	if (sub == "add" || sub == "remove") && *account == "" {
		fmt.Fprintln(stderr, "group "+sub+" requires -account")
		return 2
	}
	c, code := useClient(dial)
	if c == nil {
		return code
	}
	switch sub {
	case "members":
		got, err := c.GroupMembers(ctx, *groupID)
		return write(stdout, stderr, format, got, err)
	case "add":
		got, err := c.AddGroupMember(ctx, *groupID, *account)
		return write(stdout, stderr, format, got, err)
	case "remove":
		if err := c.RemoveGroupMember(ctx, *groupID, *account); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		res := jira.Result{OK: true, Action: "group-remove", GroupID: *groupID, AccountID: *account}
		return write(stdout, stderr, format, res, nil)
	default:
		fmt.Fprintf(stderr, "unknown group command %q\n", sub)
		return 2
	}
}

func write(stdout, stderr io.Writer, format string, v any, err error) int {
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := jira.Emit(stdout, format, v); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// reorderFlags lets the issue key sit before or after flags. flag.Parse
// stops at the first non-flag, so positionals are moved to the end and each
// flag keeps its value. Bool flags do not consume the following argument.
func reorderFlags(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		name, hasValue, ok := flagName(a)
		if !ok {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
		if hasValue || !flagTakesValue(fs, name) || i+1 >= len(args) || args[i+1] == "--" {
			continue
		}
		i++
		flags = append(flags, args[i])
	}
	return append(flags, positional...)
}

func flagName(s string) (name string, hasValue, ok bool) {
	if len(s) < 2 || s[0] != '-' {
		return "", false, false
	}
	s = strings.TrimLeft(s, "-")
	if s == "" || s[0] == '-' || s[0] == '=' {
		return "", false, false
	}
	if i := strings.IndexByte(s, '='); i >= 0 {
		return s[:i], true, true
	}
	return s, false, true
}

func flagTakesValue(fs *flag.FlagSet, name string) bool {
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	type boolFlag interface{ IsBoolFlag() bool }
	if bf, ok := f.Value.(boolFlag); ok && bf.IsBoolFlag() {
		return false
	}
	return true
}

func parseSet(fs *flag.FlagSet, args []string, stderr io.Writer) (int, bool) {
	fs.Usage = func() { fmt.Fprint(stderr, usageText) }
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, false
		}
		return 2, false
	}
	return 0, true
}

func oneArg(fs *flag.FlagSet, stderr io.Writer, cmd string) (string, bool) {
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "%s requires an issue key\n", cmd)
		return "", false
	}
	return fs.Arg(0), true
}

func visited(fs *flag.FlagSet) map[string]bool {
	seen := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { seen[f.Name] = true })
	return seen
}

func splitList(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return []string{}, nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, errors.New("empty list entry")
		}
		out = append(out, p)
	}
	return out, nil
}

const usageText = `usage: jira [-base-url URL] [-email EMAIL] [-o json|yaml|toml] <command>

  view ISSUE
  update ISSUE [-summary S] [-description TEXT] [-labels a,b]
               [-add-label a] [-remove-label b] [-due YYYY-MM-DD] [-clear-due]
               [-original-estimate 1d] [-remaining-estimate 4h]
               [-time-spent 2h] [-time-comment TEXT]
  assign ISSUE -account ACCOUNT
  unassign ISSUE
  watch list ISSUE
  watch add ISSUE [-account ACCOUNT]
  watch remove ISSUE -account ACCOUNT
  time list ISSUE
  time add ISSUE (-spent 2h | -seconds N) [-comment TEXT] [-started RFC3339]
                [-adjust auto|leave|new|manual] [-new-estimate 1h] [-reduce-by 30m]
  time update ISSUE -id ID (-spent 2h | -seconds N) [same flags as add]
  time delete ISSUE -id ID [-adjust auto|leave]
  group members -group GROUP
  group add -group GROUP -account ACCOUNT
  group remove -group GROUP -account ACCOUNT

Config file: ~/.go-jira/go-jirarc
  base_url, email, output, and api_token, in TOML.
Flags override environment variables. Environment variables override the file.
JIRA_API_TOKEN overrides api_token. The token is not a flag.
JIRA_CONFIG overrides the config path. JIRA_CONFIG=- skips the file.
`
