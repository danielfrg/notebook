package mcp

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ParseAllowedHosts accepts exact DNS names or IP addresses, never URLs or ports.
func ParseAllowedHosts(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var out []string
	for _, part := range strings.Split(value, ",") {
		host := strings.ToLower(strings.TrimSpace(part))
		if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
			host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		}
		if !validHost(host) {
			return nil, fmt.Errorf("MCP allowed hosts must contain exact hostnames or IP addresses")
		}
		out = append(out, host)
	}
	return out, nil
}

func validHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func requestHost(authority string) (string, bool) {
	u, err := url.Parse("http://" + authority)
	if err != nil || u.Host != authority || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if !validHost(host) {
		return "", false
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return "", false
		}
	} else if strings.HasSuffix(authority, ":") {
		return "", false
	}
	// IPv6 authorities must be bracketed.
	if strings.Contains(host, ":") && !strings.HasPrefix(authority, "[") {
		return "", false
	}
	return host, true
}
