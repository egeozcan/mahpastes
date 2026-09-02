package plugin

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// NetworkPolicy resolves, live, which hosts and methods a plugin may reach.
//
// Two layers, checked in order:
//
//  1. Manifest hosts — the `network = { ["host"] = {"GET"} }` table, unchanged
//     semantics including MatchDomain's single leading `*.` wildcard.
//  2. User-granted hosts — one `plugin_permissions` row per host
//     (permission_type = 'network'), created when the user saves a url-typed
//     setting. Rows match exactly, never as wildcard patterns.
//
// The security crux: a grant is a snapshot recorded when the user saved the
// field, never a lookup of the setting's current value at request time.
// Setting values live in plugin_storage, which Lua can write, so a
// value-derived rule would let a plugin self-grant arbitrary network access
// in one line. Because a grant is a row keyed to a host, a plugin rewriting
// its server_url afterwards reaches a host with no row and is denied.
//
// Allowed methods for a granted host are the union of the manifest's
// `grants_network` lists — derived from the manifest, not stored — so a DB
// row can never authorize a method the manifest never asked for.
type NetworkPolicy struct {
	db         *sql.DB
	pluginID   int64
	manifest   map[string][]string // manifest-declared host -> allowed methods
	urlMethods []string            // union of grants_network across url settings

	mu          sync.Mutex
	gen         uint64 // bumped by Invalidate on grant/revoke
	cachedGen   uint64 // generation cachedHosts was read at
	cachedHosts map[string]bool
	cacheValid  bool
}

// NewNetworkPolicy builds the live policy for one loaded plugin.
func NewNetworkPolicy(db *sql.DB, pluginID int64, manifest *Manifest) *NetworkPolicy {
	np := &NetworkPolicy{
		db:       db,
		pluginID: pluginID,
		manifest: make(map[string][]string),
	}
	if manifest != nil {
		for host, methods := range manifest.Network {
			np.manifest[host] = methods
		}
		seen := make(map[string]bool)
		for _, s := range manifest.Settings {
			if s.Type != "url" {
				continue
			}
			for _, m := range s.GrantsNetwork {
				m = strings.ToUpper(m)
				if !seen[m] {
					seen[m] = true
					np.urlMethods = append(np.urlMethods, m)
				}
			}
		}
	}
	return np
}

// Allowed reports whether a request to domain with the given HTTP method may
// proceed. Errors use the same wording the previous manifest-only check
// produced, so existing UI text and e2e assertions keep working.
func (np *NetworkPolicy) Allowed(domain, method string) error {
	// Manifest hosts first — unchanged semantics, including the `*.`
	// wildcard on the pattern side.
	if methods, ok := FindAllowedMethods(np.manifest, domain); ok {
		if methodAllowed(methods, method) {
			return nil
		}
		return fmt.Errorf("%s not allowed for domain %s (allowed: [%s])", method, domain, strings.Join(methods, ", "))
	}

	// Then user-granted hosts: exact match on the stored row. Rows are
	// normalized at grant time (lowercased, trailing dots stripped), so the
	// request host is normalized the same way before comparing — otherwise a
	// saved "https://host.example./" would show as granted yet stay denied.
	// A row containing `*` can only match a literal `*` in the request host —
	// which url.Hostname() never produces — and can never act as a wildcard.
	granted := np.grantedHosts()
	if granted[strings.ToLower(strings.TrimRight(domain, "."))] {
		if methodAllowed(np.urlMethods, method) {
			return nil
		}
		return fmt.Errorf("%s not allowed for domain %s (allowed: [%s])", method, domain, strings.Join(np.urlMethods, ", "))
	}

	return fmt.Errorf("domain not in allowlist: %s", domain)
}

// methodAllowed reports whether method appears in methods (case-insensitive).
func methodAllowed(methods []string, method string) bool {
	for _, m := range methods {
		if strings.EqualFold(m, method) {
			return true
		}
	}
	return false
}

// grantedHosts returns the set of hosts with an active (non-pending) network
// grant, cached behind the generation counter. The pending_reconfirm filter
// mirrors FilesystemAPI.loadPermissions: a restored backup flags every row,
// so an imported grant re-asks instead of silently re-granting.
func (np *NetworkPolicy) grantedHosts() map[string]bool {
	np.mu.Lock()
	defer np.mu.Unlock()
	if np.cacheValid && np.cachedGen == np.gen {
		return np.cachedHosts
	}

	hosts := make(map[string]bool)
	if np.db != nil {
		rows, err := np.db.Query(`
			SELECT path FROM plugin_permissions
			WHERE plugin_id = ? AND permission_type = 'network' AND COALESCE(pending_reconfirm, 0) = 0
		`, np.pluginID)
		if err == nil {
			for rows.Next() {
				var path string
				if err := rows.Scan(&path); err == nil && path != "" {
					hosts[strings.ToLower(path)] = true
				}
			}
			rows.Close()
		}
	}

	np.cachedGen = np.gen
	np.cachedHosts = hosts
	np.cacheValid = true
	return hosts
}

// Invalidate drops the cached grant set. Called by the grant/revoke paths so
// a permission change takes effect on the next request without a reload.
func (np *NetworkPolicy) Invalidate() {
	np.mu.Lock()
	np.gen++
	np.mu.Unlock()
}

// NormalizeGrantHost derives the host to grant from a user-supplied URL
// value. Lowercased, trailing dot stripped, port removed. A scheme-less
// value ("myserver.example:8181") is parsed as http, mirroring how plugins
// resolve their base_url. Rejects empty hosts, any host containing `*`
// (MatchDomain honours a leading `*.` on the pattern side, so an unvalidated
// user string would otherwise grant a whole zone), and anything outside
// hostname characters ([a-z0-9.-] plus IPv6 literal colons).
func NormalizeGrantHost(value string) (string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return "", fmt.Errorf("empty URL: nothing to grant")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid URL %q: %w", value, err)
	}
	host := strings.ToLower(parsed.Hostname())
	host = strings.TrimRight(host, ".")
	if host == "" {
		return "", fmt.Errorf("URL %q has no hostname", value)
	}
	if strings.Contains(host, "*") {
		return "", fmt.Errorf("hostname %q must not contain a wildcard", host)
	}
	for _, r := range host {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == ':' {
			continue
		}
		return "", fmt.Errorf("hostname %q contains invalid characters", host)
	}
	return host, nil
}
