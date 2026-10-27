package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/danielfrg/notebook/internal/mcp"
)

// VaultSpec is one configured vault: its unique name and the directory behind it.
type VaultSpec struct{ Name, Path string }

type Settings struct {
	Addr            string
	Vaults          []VaultSpec
	ReadOnly        bool
	MCPWrite        bool
	MCPAllowedHosts []string
	MCPExclude      map[string][]string // vault name -> glob patterns
}

var vaultNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func loadSettings() (Settings, error) {
	settings := Settings{Addr: env("NOTEBOOK_ADDR", "127.0.0.1:8080")}
	if _, _, err := net.SplitHostPort(settings.Addr); err != nil {
		return Settings{}, fmt.Errorf("NOTEBOOK_ADDR: %w", err)
	}
	var err error
	settings.Vaults, err = parseVaults(os.Getenv("NOTEBOOK_VAULTS"))
	if err != nil {
		return Settings{}, fmt.Errorf("NOTEBOOK_VAULTS: %w", err)
	}
	settings.ReadOnly, err = strconv.ParseBool(env("NOTEBOOK_READONLY", "false"))
	if err != nil {
		return Settings{}, fmt.Errorf("NOTEBOOK_READONLY: %w", err)
	}
	settings.MCPWrite, err = strconv.ParseBool(env("NOTEBOOK_MCP_WRITE", "false"))
	if err != nil {
		return Settings{}, fmt.Errorf("NOTEBOOK_MCP_WRITE: %w", err)
	}
	settings.MCPAllowedHosts, err = mcp.ParseAllowedHosts(os.Getenv("NOTEBOOK_MCP_ALLOWED_HOSTS"))
	if err != nil {
		return Settings{}, fmt.Errorf("NOTEBOOK_MCP_ALLOWED_HOSTS: %w", err)
	}
	settings.MCPExclude, err = parseExcludes(os.Getenv("NOTEBOOK_MCP_EXCLUDE"), settings.Vaults)
	if err != nil {
		return Settings{}, fmt.Errorf("NOTEBOOK_MCP_EXCLUDE: %w", err)
	}
	return settings, nil
}

// parseVaults reads name=path,name2=path2. Names are unique and match
// vaultNamePattern; relative paths become absolute.
func parseVaults(value string) ([]VaultSpec, error) {
	if strings.TrimSpace(value) == "" {
		return nil, errors.New("at least one vault is required, for example notes=/path/to/notes")
	}
	var vaults []VaultSpec
	seen := map[string]bool{}
	for _, entry := range strings.Split(value, ",") {
		name, path, ok := strings.Cut(entry, "=")
		name, path = strings.TrimSpace(name), strings.TrimSpace(path)
		if !ok {
			return nil, fmt.Errorf("vault entry %q must look like name=path", strings.TrimSpace(entry))
		}
		if !vaultNamePattern.MatchString(name) {
			return nil, fmt.Errorf("vault name %q must match %s", name, vaultNamePattern)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate vault name %q", name)
		}
		if path == "" {
			return nil, fmt.Errorf("vault %q has no path", name)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("vault %q: %w", name, err)
		}
		seen[name] = true
		vaults = append(vaults, VaultSpec{Name: name, Path: abs})
	}
	return vaults, nil
}

// parseExcludes reads vault:pattern,vault:pattern. Patterns are relative to
// the vault root, so absolute patterns and ".." segments are rejected: an
// exclude that points outside the vault could never hide what it was meant to.
func parseExcludes(value string, vaults []VaultSpec) (map[string][]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	excludes := map[string][]string{}
	for _, entry := range strings.Split(value, ",") {
		name, pattern, ok := strings.Cut(entry, ":")
		name, pattern = strings.TrimSpace(name), strings.TrimSpace(pattern)
		if !ok {
			return nil, fmt.Errorf("exclude %q must look like vault:pattern", strings.TrimSpace(entry))
		}
		if !slices.ContainsFunc(vaults, func(v VaultSpec) bool { return v.Name == name }) {
			return nil, fmt.Errorf("exclude %q names an unknown vault", strings.TrimSpace(entry))
		}
		switch {
		case pattern == "":
			return nil, fmt.Errorf("exclude for vault %q has an empty pattern", name)
		case strings.HasPrefix(pattern, "/"):
			return nil, fmt.Errorf("exclude pattern %q must be relative to the vault", pattern)
		case slices.Contains(strings.Split(pattern, "/"), ".."):
			return nil, fmt.Errorf("exclude pattern %q must not contain ..", pattern)
		}
		excludes[name] = append(excludes[name], pattern)
	}
	return excludes, nil
}
