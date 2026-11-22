package api

import (
	"encoding/hex"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"panel/internal/brand"
)

// publicAddrs lists this machine's public unicast addresses: what a hostname has to
// resolve to for its traffic to arrive here.
//
// In a container the panel's own interfaces carry bridge addresses only, so on their own
// they never name a public one. When the host's /proc is mounted (the installer mounts it
// at /host/proc for the host metrics) the addresses of the host's network namespace are
// read from there as well.
func publicAddrs() []string {
	seen := map[string]bool{}
	add := func(ip netip.Addr) {
		ip = ip.Unmap()
		if ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() {
			seen[ip.String()] = true
		}
	}
	if ifaces, err := net.InterfaceAddrs(); err == nil {
		for _, a := range ifaces {
			if ipnet, ok := a.(*net.IPNet); ok {
				if ip, ok := netip.AddrFromSlice(ipnet.IP); ok {
					add(ip)
				}
			}
		}
	}
	if root := hostProcRoot(); root != "" {
		// Process 1 of the host lives in the host's network namespace.
		if b, err := os.ReadFile(filepath.Join(root, "1/net/fib_trie")); err == nil {
			for _, ip := range parseFibTrieLocal(string(b)) {
				add(ip)
			}
		}
		if b, err := os.ReadFile(filepath.Join(root, "1/net/if_inet6")); err == nil {
			for _, ip := range parseIfInet6(string(b)) {
				add(ip)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for ip := range seen {
		out = append(out, ip)
	}
	sort.Strings(out)
	return out
}

// hostProcRoot is where the host's /proc is mounted, or "" when it is not: the same
// place the host metrics read from.
func hostProcRoot() string {
	if root := os.Getenv(brand.EnvPrefix() + "HOST_PROC"); root != "" {
		return root
	}
	if _, err := os.Stat("/host/proc/stat"); err == nil {
		return "/host/proc"
	}
	return ""
}

// parseFibTrieLocal reads the IPv4 addresses assigned to a machine out of
// /proc/net/fib_trie: the leaves marked "/32 host LOCAL".
//
//	|-- 203.0.113.5
//	   /32 host LOCAL
func parseFibTrieLocal(s string) []netip.Addr {
	var out []netip.Addr
	var leaf netip.Addr
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "|-- "); ok {
			leaf, _ = netip.ParseAddr(rest)
			continue
		}
		if leaf.IsValid() && strings.HasPrefix(line, "/32 host LOCAL") {
			out = append(out, leaf)
		}
	}
	return out
}

// parseIfInet6 reads the IPv6 addresses of /proc/net/if_inet6: 32 hex digits, then the
// interface index, prefix length, scope, flags and name.
func parseIfInet6(s string) []netip.Addr {
	var out []netip.Addr
	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || len(fields[0]) != 32 {
			continue
		}
		b, err := hex.DecodeString(fields[0])
		if err != nil {
			continue
		}
		if ip, ok := netip.AddrFromSlice(b); ok {
			out = append(out, ip)
		}
	}
	return out
}
