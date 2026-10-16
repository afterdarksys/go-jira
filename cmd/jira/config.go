package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/pelletier/go-toml/v2"
)

const maxConfigSize = 16 << 10

type fileConfig struct {
	BaseURL  string `toml:"base_url"`
	Email    string `toml:"email"`
	Output   string `toml:"output"`
	APIToken string `toml:"api_token"`
}

// loadConfig reads ~/.go-jira/go-jirarc. JIRA_CONFIG selects another path.
// JIRA_CONFIG=- skips the file. A missing default file is empty. A missing
// explicit path is an error.
//
// Threats: the file can hold the API token. Symlinks, non-regular files,
// oversized files, a group- or world-writable directory, and a file that is
// not owner-only are rejected. Parse failures return a fixed error so a
// broken file cannot echo the token. The file is not encrypted at rest.
func loadConfig(env map[string]string) (cfg fileConfig, err error) {
	var zero fileConfig
	explicit := false
	var path string
	if p, ok := env["JIRA_CONFIG"]; ok {
		if p == "" || p == "-" {
			return zero, nil
		}
		path = p
		explicit = true
	} else {
		home := env["HOME"]
		if home == "" {
			return zero, errors.New("HOME is not set")
		}
		path = filepath.Join(home, ".go-jira", "go-jirarc")
	}

	lst, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if explicit {
				return zero, errors.New("go-jirarc not found")
			}
			return zero, nil
		}
		return zero, errors.New("go-jirarc is not available")
	}
	if lst.Mode()&os.ModeSymlink != 0 {
		return zero, errors.New("go-jirarc must not be a symlink")
	}
	if !lst.Mode().IsRegular() {
		return zero, errors.New("go-jirarc must be a regular file")
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil || !dirInfo.IsDir() {
		return zero, errors.New("go-jirarc directory is not available")
	}
	if dirInfo.Mode().Perm()&0o022 != 0 {
		return zero, errors.New("go-jirarc directory must not be writable by group or others")
	}

	f, err := os.Open(path)
	if err != nil {
		return zero, errors.New("go-jirarc is not available")
	}
	defer func() {
		cerr := f.Close()
		if err == nil && cerr != nil {
			cfg = zero
			err = errors.New("go-jirarc is not available")
		}
	}()

	fst, err := f.Stat()
	if err != nil {
		return zero, errors.New("go-jirarc is not available")
	}
	if !os.SameFile(lst, fst) || !fst.Mode().IsRegular() {
		return zero, errors.New("go-jirarc changed while opening")
	}
	uid := os.Getuid()
	st, ok := fst.Sys().(*syscall.Stat_t)
	if uid < 0 || !ok || st.Uid != uint32(uid) {
		return zero, errors.New("go-jirarc must be owned by the current user")
	}
	if fst.Mode().Perm()&0o077 != 0 {
		return zero, errors.New("go-jirarc must be readable only by its owner")
	}

	buf, err := io.ReadAll(io.LimitReader(f, maxConfigSize+1))
	if err != nil {
		return zero, errors.New("go-jirarc is not available")
	}
	if len(buf) > maxConfigSize {
		return zero, errors.New("go-jirarc is too large")
	}

	dec := toml.NewDecoder(bytes.NewReader(buf))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return zero, errors.New("invalid go-jirarc")
	}
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.Email = strings.TrimSpace(cfg.Email)
	cfg.Output = strings.TrimSpace(cfg.Output)
	cfg.APIToken = strings.TrimSpace(cfg.APIToken)
	return cfg, nil
}
