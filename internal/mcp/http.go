package mcp

import (
	"net/http"
	"net/url"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// protect lets through the requests that a client of an MCP endpoint makes and a
// page of another site cannot: POSTs for a host that is localhost or one of
// hosts, and, if they say where from, from that host.
func protect(next http.Handler, hosts []string) http.Handler {
	allowed := map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}
	for _, host := range hosts {
		allowed[strings.ToLower(strings.Trim(host, "[]"))] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		host, ok := requestHost(r.Host)
		if !ok || !allowed[host] {
			http.Error(w, "MCP host is not allowed", http.StatusForbidden)
			return
		}
		if origins, exists := r.Header["Origin"]; exists {
			origin := ""
			if len(origins) == 1 {
				origin = origins[0]
			}
			u, err := url.Parse(origin)
			if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Host, r.Host) ||
				u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(origin, "#") {
				http.Error(w, "MCP origin is not allowed", http.StatusForbidden)
				return
			}
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Use Streamable HTTP POST for MCP", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// transport serves server as Streamable HTTP behind protect: stateless, so no
// session is kept, with every answer a single JSON document and a request of at
// most 1 MiB.
func transport(server *sdk.Server, hosts []string) http.Handler {
	return protect(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{
		Stateless:                    true,
		JSONResponse:                 true,
		DisableLocalhostProtection:   true, // protect has done that, for the hosts that were configured too
		PropagateRequestCancellation: true,
		MaxRequestBodyBytes:          1 << 20,
	}), hosts)
}
