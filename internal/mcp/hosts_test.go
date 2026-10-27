package mcp

import (
	"reflect"
	"testing"
)

func TestParseAllowedHosts(t *testing.T) {
	for _, value := range []string{"https://example.net", "*.example.net", "bad:8080", "a,,b"} {
		if _, err := ParseAllowedHosts(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}

	got, err := ParseAllowedHosts("example.net, [::1]")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"example.net", "::1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Names are lowercased; a blank value means localhost only.
	got, err = ParseAllowedHosts("Notes.Example.NET")
	if err != nil || !reflect.DeepEqual(got, []string{"notes.example.net"}) {
		t.Errorf("got %v, %v; want [notes.example.net]", got, err)
	}
	for _, value := range []string{"", "  "} {
		got, err := ParseAllowedHosts(value)
		if err != nil || got != nil {
			t.Errorf("ParseAllowedHosts(%q) = %v, %v; want nil, nil", value, got, err)
		}
	}
}

func TestValidHost(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost":    true,
		"example.net":  true,
		"a-b.example":  true,
		"127.0.0.1":    true,
		"::1":          true,
		"":             false,
		"-a.example":   false,
		"a-.example":   false,
		"a..example":   false,
		"a b.example":  false,
		"*.example":    false,
		"example.net/": false,
	} {
		if got := validHost(host); got != want {
			t.Errorf("validHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestRequestHost(t *testing.T) {
	for authority, want := range map[string]string{
		"localhost:8080":  "localhost",
		"[::1]:8080":      "::1",
		"[::1]":           "::1",
		"example.net":     "example.net",
		"Example.NET:443": "example.net",
	} {
		if got, ok := requestHost(authority); !ok || got != want {
			t.Errorf("requestHost(%q) = %q, %v; want %q, true", authority, got, ok, want)
		}
	}
	for _, authority := range []string{
		"",
		"example.net:",      // empty port
		"example.net:0",     // port below range
		"example.net:99999", // port above range
		"user@example.net",  // userinfo
		"example.net/path",  // not an authority
		"::1",               // IPv6 must be bracketed
		"::1:8080",          // unbracketed IPv6 with a port is ambiguous
		"exa mple.net",      // invalid host
	} {
		if got, ok := requestHost(authority); ok {
			t.Errorf("requestHost(%q) = %q, true; want rejection", authority, got)
		}
	}
}
