package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"

	"panel/internal/account"
	"panel/internal/brand"
	"panel/internal/store"
)

// isAdmin is true for the owner and admins: the roles that may reach the host.
func isAdmin(u store.User) bool { return u.Role == account.RoleOwner || u.Role == account.RoleAdmin }

// reservedVolume reports whether name is a Docker volume the panel or its builders own:
// the data volume, the built-in storage and registry, database instances (all named after
// the product) and BuildKit's state. Mounting one into an app reads what the roles keep
// from a maintainer: other projects' database files, backups, and with a data volume the
// panel's own database and key.
func reservedVolume(name string) bool {
	n := strings.ToLower(name)
	return n == brand.Slug || strings.HasPrefix(n, brand.Slug+"-") || strings.HasPrefix(n, brand.Slug+"_") ||
		strings.HasPrefix(n, "buildx_") || strings.HasPrefix(n, "buildkit")
}

func hasBind(in store.AppInput) bool {
	return slices.ContainsFunc(in.Mounts, func(m store.AppMount) bool { return m.Type == "bind" })
}

// hostAccessProblem says why saving next over prev needs an admin; "" when it does not.
// Three app settings hand out the host: a bind mount exposes any host path (the Docker
// socket included), a volume the panel owns holds its own data, and a Buildpacks builder
// image runs as root with the Docker socket. A maintainer may keep what an admin set, but
// not add or change any of them. An app that already has a host path is the admin's as a
// whole: whoever picks what runs in it reaches that path.
func hostAccessProblem(prev, next store.AppInput) string {
	for _, m := range next.Mounts {
		kept := slices.ContainsFunc(prev.Mounts, func(p store.AppMount) bool {
			return p.Type == m.Type && p.Source == m.Source && p.Target == m.Target && p.ReadOnly == m.ReadOnly
		})
		if kept {
			continue
		}
		switch {
		case m.Type == "bind":
			return "your role cannot mount host paths; use a volume, or ask an admin"
		case m.Type == "volume" && reservedVolume(m.Source):
			return "the volume " + m.Source + " belongs to the panel; pick another name, or ask an admin"
		}
	}
	builderImage := func(in store.AppInput) string {
		if in.SourceType != store.SourceGit || in.Git.Builder != store.BuilderBuildpacks {
			return ""
		}
		return in.Git.Options.Image
	}
	if image := builderImage(next); image != "" && image != builderImage(prev) {
		return "your role cannot choose the Buildpacks builder image; ask an admin"
	}
	if hasBind(prev) && (prev.SourceType != next.SourceType || prev.Image != next.Image || prev.RegistryID != next.RegistryID ||
		prev.StartCommand != next.StartCommand || !reflect.DeepEqual(prev.Git, next.Git) || !sameProcesses(prev.Processes, next.Processes)) {
		return "this app mounts a host path, so only an admin can change its source, image, start command or processes"
	}
	return ""
}

func sameProcesses(a, b []store.AppProcess) bool {
	return len(a) == len(b) && (len(a) == 0 || reflect.DeepEqual(a, b))
}

// mayChangeApp answers 403 and reports false when the user's role cannot save next over prev.
func (s *Server) mayChangeApp(w http.ResponseWriter, r *http.Request, prev, next store.AppInput) bool {
	if isAdmin(userFrom(r.Context())) {
		return true
	}
	if msg := hostAccessProblem(prev, next); msg != "" {
		writeError(w, http.StatusForbidden, "forbidden", msg)
		return false
	}
	// A repository on the server's own network: the clone would be the panel calling in.
	if next.SourceType == store.SourceGit && next.Git.Repository != prev.Git.Repository {
		if u, err := url.Parse(next.Git.Repository); err == nil && u.Scheme == "https" && !s.mayReach(w, r, u.Host) {
			return false
		}
	}
	return true
}

// cgnat is the shared address space of carrier NAT (and of some private overlays).
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

func internalAddr(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsMulticast() || a.IsUnspecified() || cgnat.Contains(a)
}

// mayReach answers 403 and reports false when hostport names an address inside the
// server's own networks (loopback, private ranges, the cloud metadata address, other
// containers) and the user is not an admin: the panel would make the request from where
// those are reachable. A name that does not resolve is let through; the request then
// fails on its own. This looks the name up once: it narrows what a maintainer can aim
// the panel at, it is not a firewall.
func (s *Server) mayReach(w http.ResponseWriter, r *http.Request, hostport string) bool {
	if isAdmin(userFrom(r.Context())) {
		return true
	}
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	var addrs []string
	if _, err := netip.ParseAddr(host); err == nil {
		addrs = []string{host}
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		addrs, _ = s.lookupHost(ctx, host)
		cancel()
	}
	for _, raw := range addrs {
		if a, err := netip.ParseAddr(raw); err == nil && internalAddr(a) {
			writeError(w, http.StatusForbidden, "forbidden", host+" is an address inside the server's own network; ask an admin to add it")
			return false
		}
	}
	return true
}

// retiredPanelDomains are hostnames the panel used to answer on. Browsers that signed in
// there still hold a session cookie for them, so whoever serves such a name next receives
// those cookies: only an admin may route one to an app.
func (s *Server) retiredPanelDomains(ctx context.Context) ([]string, error) {
	raw, _, err := s.store.GetSetting(ctx, store.SettingPanelRetired)
	if err != nil || raw == "" {
		return nil, err
	}
	var hosts []string
	if json.Unmarshal([]byte(raw), &hosts) != nil {
		return nil, nil
	}
	return hosts, nil
}

// retirePanelDomain records old as a former panel hostname and drops current, the one in
// use now, from the list.
func (s *Server) retirePanelDomain(ctx context.Context, old, current string) error {
	hosts, err := s.retiredPanelDomains(ctx)
	if err != nil {
		return err
	}
	if old != "" && !slices.Contains(hosts, old) {
		hosts = append(hosts, old)
	}
	hosts = slices.DeleteFunc(hosts, func(h string) bool { return h == current })
	if len(hosts) > 50 { // keep the newest; a list this long is not bookmarks any more
		hosts = hosts[len(hosts)-50:]
	}
	raw, err := json.Marshal(hosts)
	if err != nil {
		return err
	}
	return s.store.SetSetting(ctx, store.SettingPanelRetired, string(raw))
}

// retiredHostProblem says why the user cannot serve host from an app; "" when they can.
// With wildcard, host is a domain whose every subdomain would be served.
func (s *Server) retiredHostProblem(r *http.Request, host string, wildcard bool) (string, error) {
	if isAdmin(userFrom(r.Context())) {
		return "", nil
	}
	hosts, err := s.retiredPanelDomains(r.Context())
	if err != nil {
		return "", err
	}
	for _, h := range hosts {
		if h == host && !wildcard {
			return host + " used to be the panel's own domain; ask an admin to add it", nil
		}
		if wildcard && strings.HasSuffix(h, "."+host) {
			return h + " used to be the panel's own domain; ask an admin to add this wildcard route", nil
		}
	}
	return "", nil
}
