package jira

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	issueKeyRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{1,19}-[0-9]{1,18}$`)
	issueIDRE  = regexp.MustCompile(`^[0-9]{1,18}$`)
	objectIDRE = regexp.MustCompile(`^[A-Za-z0-9:._-]{1,128}$`)
	durationRE = regexp.MustCompile(`^(?:([0-9]+)w)?(?:([0-9]+)d)?(?:([0-9]+)h)?(?:([0-9]+)m)?$`)
)

func validEmail(s string) bool {
	if len(s) < 3 || len(s) > 254 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	at := strings.LastIndex(s, "@")
	if at <= 0 || at >= len(s)-1 {
		return false
	}
	domain := s[at+1:]
	return strings.Contains(domain, ".") && !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}

func validToken(s string) bool {
	if len(s) < 8 || len(s) > 4096 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e || s[i] == ':' {
			return false
		}
	}
	return true
}

func validateIssue(ref string) error {
	if issueKeyRE.MatchString(ref) || issueIDRE.MatchString(ref) {
		return nil
	}
	return errors.New("jira: invalid issue key")
}

func issuePath(issue, rest string) (string, error) {
	if err := validateIssue(issue); err != nil {
		return "", err
	}
	return "/rest/api/3/issue/" + url.PathEscape(issue) + rest, nil
}

func validateObjectID(kind, id string) error {
	if objectIDRE.MatchString(id) {
		return nil
	}
	return errors.New("jira: invalid " + kind)
}

func validateLabel(s string) error {
	if s == "" || len(s) > 255 {
		return errors.New("jira: invalid label")
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == ',' {
			return errors.New("jira: invalid label")
		}
	}
	return nil
}

func validateLabels(list []string) error {
	if len(list) > 100 {
		return errors.New("jira: too many labels")
	}
	seen := make(map[string]struct{}, len(list))
	for _, s := range list {
		if err := validateLabel(s); err != nil {
			return err
		}
		if _, ok := seen[s]; ok {
			return errors.New("jira: duplicate label")
		}
		seen[s] = struct{}{}
	}
	return nil
}

func validateDue(s string) error {
	t, err := time.Parse("2006-01-02", s)
	if err != nil || t.Format("2006-01-02") != s {
		return errors.New("jira: invalid due date")
	}
	return nil
}

const maxDurationSeconds = 365 * 24 * 3600

// normalizeDuration checks a Jira duration (2h, 1d 4h, 30m).
// A bare positive integer is a minute count: "90" becomes "90m".
func normalizeDuration(s string) (string, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	if s == "" || len(s) > 32 {
		return "", errors.New("jira: invalid duration")
	}
	if durationRE.MatchString(s) && strings.ContainsAny(s, "wdhm") {
		m := durationRE.FindStringSubmatch(s)
		secs := 0
		mult := []int{7 * 24 * 3600, 24 * 3600, 3600, 60}
		for i, part := range m[1:] {
			if part == "" {
				continue
			}
			n, err := strconv.Atoi(part)
			if err != nil || n < 0 || n > 100000 {
				return "", errors.New("jira: invalid duration")
			}
			secs += n * mult[i]
		}
		if secs == 0 || secs > maxDurationSeconds {
			return "", errors.New("jira: invalid duration")
		}
		return s, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || n > maxDurationSeconds/60 {
		return "", errors.New("jira: invalid duration")
	}
	return strconv.Itoa(n) + "m", nil
}

func validateSummary(s string) error {
	if s == "" || len(s) > 255 {
		return errors.New("jira: invalid summary")
	}
	if strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return errors.New("jira: invalid summary")
	}
	return nil
}

func validateText(kind, s string, max int) error {
	if len(s) > max {
		return errors.New("jira: " + kind + " is too long")
	}
	if strings.ContainsRune(s, 0) {
		return errors.New("jira: invalid " + kind)
	}
	return nil
}
