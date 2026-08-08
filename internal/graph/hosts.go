package graph

import (
	"net/url"
	"strings"
)

// ExtractHostname returns a bare hostname from a literal env/command value.
// Never returns secrets or full URLs — host only (RT-013). Empty if not parseable.
func ExtractHostname(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	// Reject obvious non-hosts / paths / bare numbers.
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, ".") {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
		if isPlausibleHost(host) {
			return host
		}
	}
	// Scheme-less host:port or bare host (common DATABASE_HOST=db.example.com).
	for _, field := range strings.Fields(value) {
		field = strings.Trim(field, `"'(),[]`)
		if i := strings.LastIndex(field, "="); i >= 0 {
			field = field[i+1:]
		}
		if strings.HasPrefix(field, "/") {
			continue
		}
		if i := strings.IndexByte(field, '/'); i >= 0 {
			field = field[:i]
		}
		host := field
		if h, _, ok := strings.Cut(field, ":"); ok {
			host = h
		}
		host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
		if isPlausibleHost(host) {
			return host
		}
	}
	return ""
}

func isPlausibleHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" || len(host) > 253 {
		return false
	}
	// Need a dot (external FQDN or svc.ns) or kubernetes DNS short name with letters.
	if strings.ContainsAny(host, " \t\n@$") {
		return false
	}
	if strings.Contains(host, ".") {
		return true
	}
	// Short DNS label (in-cluster Service name) — letters/digits/hyphen only.
	for _, r := range host {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return len(host) >= 2
}

// IsClusterLocalHost reports whether host looks like in-cluster Service DNS.
func IsClusterLocalHost(host, namespace string) (svcName string, ok bool) {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	ns := strings.ToLower(strings.TrimSpace(namespace))
	if host == "" {
		return "", false
	}
	suffixes := []string{}
	if ns != "" {
		suffixes = []string{
			"." + ns + ".svc.cluster.local",
			"." + ns + ".svc",
			"." + ns,
		}
	}
	suffixes = append(suffixes, ".svc.cluster.local", ".svc")
	for _, suf := range suffixes {
		if strings.HasSuffix(host, suf) {
			name := strings.TrimSuffix(host, suf)
			if name != "" && !strings.Contains(name, ".") {
				return name, true
			}
		}
	}
	// Bare short name — caller may match against known Services.
	if !strings.Contains(host, ".") {
		return host, true
	}
	return "", false
}
