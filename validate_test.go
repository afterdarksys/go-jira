package jira

import "testing"

func TestNormalizeDuration(t *testing.T) {
	ok := map[string]string{
		"2h":      "2h",
		"1d 30m":  "1d30m",
		"90":      "90m",
		"52w":     "52w",
		"  15m  ": "15m",
	}
	for in, want := range ok {
		got, err := normalizeDuration(in)
		if err != nil || got != want {
			t.Fatalf("%q -> %q %v", in, got, err)
		}
	}
	for _, in := range []string{"", "0m", "0", "53w", "1x", "1.5h", "-5"} {
		if _, err := normalizeDuration(in); err == nil {
			t.Fatalf("accepted %q", in)
		}
	}
}

func TestValidateIssueAndLabels(t *testing.T) {
	for _, s := range []string{"PROJ-1", "ab-1", "10001"} {
		if err := validateIssue(s); err != nil {
			t.Fatal(s, err)
		}
	}
	for _, s := range []string{"", "a-1", "PROJ-1/../x", "DROP TABLE", "PR OJ-1"} {
		if err := validateIssue(s); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	if err := validateLabels([]string{"a", "a"}); err == nil {
		t.Fatal("duplicate")
	}
	if err := validateLabel("has space"); err == nil {
		t.Fatal("space")
	}
	if err := validateLabel("a,b"); err == nil {
		t.Fatal("comma")
	}
	many := make([]string, 101)
	for i := range many {
		many[i] = "l"
	}
	// duplicates fail first; use unique values
	for i := range many {
		many[i] = string(rune('a'+i%26)) + string(rune('0'+i%10)) + itoa(i)
	}
	if err := validateLabels(many); err == nil {
		t.Fatal("too many")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
