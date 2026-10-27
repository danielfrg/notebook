package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// isolate blanks every NOTEBOOK_* variable so a developer's shell or .env
// cannot leak into a test. An empty value counts as unset.
func isolate(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"NOTEBOOK_ADDR",
		"NOTEBOOK_VAULTS",
		"NOTEBOOK_READONLY",
		"NOTEBOOK_MCP_WRITE",
		"NOTEBOOK_MCP_ALLOWED_HOSTS",
		"NOTEBOOK_MCP_EXCLUDE",
	} {
		t.Setenv(name, "")
	}
}

// load isolates the environment, configures one vault plus the given
// variables, and loads the settings.
func load(t *testing.T, env map[string]string) (Settings, error) {
	t.Helper()
	isolate(t)
	t.Setenv("NOTEBOOK_VAULTS", "notes=/tmp/n")
	for name, value := range env {
		t.Setenv(name, value)
	}
	return loadSettings()
}

func TestLoadSettingsDefaults(t *testing.T) {
	isolate(t)
	t.Setenv("NOTEBOOK_VAULTS", "notes=/tmp/n")
	s, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s.Addr != "127.0.0.1:8080" {
		t.Errorf("Addr = %q, want 127.0.0.1:8080", s.Addr)
	}
	if s.ReadOnly || s.MCPWrite {
		t.Errorf("ReadOnly = %v, MCPWrite = %v, want both false", s.ReadOnly, s.MCPWrite)
	}
	if s.MCPAllowedHosts != nil {
		t.Errorf("MCPAllowedHosts = %v, want nil", s.MCPAllowedHosts)
	}
	if len(s.MCPExclude) != 0 {
		t.Errorf("MCPExclude = %v, want none", s.MCPExclude)
	}
	if want := []VaultSpec{{"notes", "/tmp/n"}}; !reflect.DeepEqual(s.Vaults, want) {
		t.Errorf("Vaults = %v, want %v", s.Vaults, want)
	}
}

func TestLoadSettingsRequiresVaults(t *testing.T) {
	isolate(t)
	_, err := loadSettings()
	if err == nil || !strings.Contains(err.Error(), "NOTEBOOK_VAULTS") {
		t.Fatalf("error = %v, want one naming NOTEBOOK_VAULTS", err)
	}
}

func TestLoadSettingsInvalidVaults(t *testing.T) {
	_, err := load(t, map[string]string{"NOTEBOOK_VAULTS": "Bad Name=/x"})
	if err == nil || !strings.Contains(err.Error(), "NOTEBOOK_VAULTS") {
		t.Fatalf("error = %v, want one naming NOTEBOOK_VAULTS", err)
	}
}

func TestLoadSettingsBooleans(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"NOTEBOOK_READONLY", "maybe"},
		{"NOTEBOOK_MCP_WRITE", "x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, map[string]string{tc.name: tc.value})
			if err == nil || !strings.Contains(err.Error(), tc.name) {
				t.Fatalf("error = %v, want one naming %s", err, tc.name)
			}
		})
	}
	s, err := load(t, map[string]string{"NOTEBOOK_READONLY": "true", "NOTEBOOK_MCP_WRITE": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if !s.ReadOnly || !s.MCPWrite {
		t.Errorf("ReadOnly = %v, MCPWrite = %v, want both true", s.ReadOnly, s.MCPWrite)
	}
}

func TestLoadSettingsAddr(t *testing.T) {
	s, err := load(t, map[string]string{"NOTEBOOK_ADDR": ":9090"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Addr != ":9090" {
		t.Errorf("Addr = %q, want :9090", s.Addr)
	}
	_, err = load(t, map[string]string{"NOTEBOOK_ADDR": "localhost"})
	if err == nil || !strings.Contains(err.Error(), "NOTEBOOK_ADDR") {
		t.Fatalf("error = %v, want one naming NOTEBOOK_ADDR", err)
	}
}

func TestParseVaults(t *testing.T) {
	rel, err := filepath.Abs("./rel")
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseVaults("a=/x,b=./rel")
	if err != nil {
		t.Fatal(err)
	}
	if want := []VaultSpec{{"a", "/x"}, {"b", rel}}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Entries keep their order, tolerate surrounding spaces, and split on the
	// first "=" only.
	got, err = parseVaults(" notes=/n , work=/w=1 ")
	if err != nil {
		t.Fatal(err)
	}
	if want := []VaultSpec{{"notes", "/n"}, {"work", "/w=1"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// The longest allowed name is 32 characters.
	if _, err := parseVaults(strings.Repeat("a", 32) + "=/x"); err != nil {
		t.Errorf("rejected a 32-character name: %v", err)
	}

	for _, value := range []string{
		"",                              // nothing configured
		"a",                             // no path
		"a=",                            // empty path
		"=/x",                           // empty name
		"Bad Name=/x",                   // uppercase and space
		"-a=/x",                         // must start with a letter or digit
		"a=/x,a=/y",                     // duplicate name
		"a=/x,",                         // empty entry
		strings.Repeat("a", 33) + "=/x", // longer than 32 characters
	} {
		if _, err := parseVaults(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}

func TestParseExcludes(t *testing.T) {
	notes := []VaultSpec{{"notes", "/tmp/n"}}
	got, err := parseExcludes("notes:transcripts,notes:Private/**", notes)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string][]string{"notes": {"transcripts", "Private/**"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Several vaults and spaces around entries are fine.
	two := []VaultSpec{{"notes", "/tmp/n"}, {"work", "/tmp/w"}}
	got, err = parseExcludes("notes:a, work:b/*.md ,notes:c", two)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"notes": {"a", "c"}, "work": {"b/*.md"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// No value means nothing is excluded.
	for _, value := range []string{"", "  "} {
		got, err := parseExcludes(value, notes)
		if err != nil || len(got) != 0 {
			t.Errorf("parseExcludes(%q) = %v, %v; want no excludes", value, got, err)
		}
	}

	// A pattern is a vault-relative glob matched segment by segment. It is
	// stored in canonical form: one trailing slash is trimmed and nothing else
	// is rewritten.
	for pattern, canonical := range map[string]string{
		"transcripts":     "transcripts",
		"transcripts/":    "transcripts",
		" transcripts/ ":  "transcripts",
		"Private/**":      "Private/**",
		"Private/**/":     "Private/**",
		"**/*.private.md": "**/*.private.md",
		"a/*/c.md":        "a/*/c.md",
		"a b/c?":          "a b/c?",
		"Q1..Q2":          "Q1..Q2", // dots inside a name are not "." or ".." segments
		"..data":          "..data",
		".obsidian":       ".obsidian",
	} {
		got, err := parseExcludes("notes:"+pattern, notes)
		want := map[string][]string{"notes": {canonical}}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("parseExcludes(%q) = %v, %v; want %v", "notes:"+pattern, got, err, want)
		}
	}

	for _, value := range []string{
		"x:a",              // unknown vault
		"notes",            // missing colon
		"notes:a,,notes:b", // empty entry

		// Patterns that could never match a segment-based glob would silently
		// hide nothing from MCP, so they are rejected.
		"notes:",              // empty
		"notes:a,notes:",      // empty after a valid one
		"notes:/",             // nothing but a slash
		"notes:/abs",          // absolute
		"notes:/abs/",         // absolute with a trailing slash
		"notes:../x",          // escapes the vault
		"notes:../up",         // escapes the vault
		"notes:a/../b",        // parent segment in the middle
		"notes:..",            // parent segment alone
		"notes:a/..",          // parent segment at the end
		"notes:./transcripts", // "." segment
		"notes:a/./b",         // "." segment in the middle
		"notes:.",             // "." segment alone
		"notes:a/.",           // "." segment at the end
		"notes:a//b",          // empty segment
		"notes:a//",           // empty segment left after one trailing slash is trimmed
		`notes:a\b`,           // backslash
		`notes:a\`,            // trailing backslash
	} {
		if _, err := parseExcludes(value, notes); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}

func TestLoadSettingsMCPHosts(t *testing.T) {
	s, err := load(t, map[string]string{"NOTEBOOK_MCP_ALLOWED_HOSTS": "a.example,[::1]"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.example", "::1"}; !reflect.DeepEqual(s.MCPAllowedHosts, want) {
		t.Errorf("MCPAllowedHosts = %v, want %v", s.MCPAllowedHosts, want)
	}
	_, err = load(t, map[string]string{"NOTEBOOK_MCP_ALLOWED_HOSTS": "https://x"})
	if err == nil || !strings.Contains(err.Error(), "NOTEBOOK_MCP_ALLOWED_HOSTS") {
		t.Fatalf("error = %v, want one naming NOTEBOOK_MCP_ALLOWED_HOSTS", err)
	}
}

func TestLoadSettingsMCPExclude(t *testing.T) {
	vaults := "notes=/tmp/n,work=/tmp/w"
	s, err := load(t, map[string]string{
		"NOTEBOOK_VAULTS":      vaults,
		"NOTEBOOK_MCP_EXCLUDE": "notes:transcripts,work:Private/**",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"notes": {"transcripts"}, "work": {"Private/**"}}
	if !reflect.DeepEqual(s.MCPExclude, want) {
		t.Errorf("MCPExclude = %v, want %v", s.MCPExclude, want)
	}
	// A bad exclude stops startup instead of silently hiding nothing.
	for _, value := range []string{"ghost:a", "notes:./transcripts"} {
		_, err = load(t, map[string]string{"NOTEBOOK_VAULTS": vaults, "NOTEBOOK_MCP_EXCLUDE": value})
		if err == nil || !strings.Contains(err.Error(), "NOTEBOOK_MCP_EXCLUDE") {
			t.Errorf("%q: error = %v, want one naming NOTEBOOK_MCP_EXCLUDE", value, err)
		}
	}
}
