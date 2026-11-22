package api

import "net/netip"

// cloudflareRanges are the addresses Cloudflare's proxy answers on, as published at
// https://www.cloudflare.com/ips/ (read 2026-11-22). The list changes rarely; an address
// from a range added since is reported as any other unknown address, not as an error.
var cloudflareRanges = mustPrefixes(
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22", "141.101.64.0/18",
	"108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20", "197.234.240.0/22", "198.41.128.0/17",
	"162.158.0.0/15", "104.16.0.0/13", "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32", "2405:8100::/32",
	"2a06:98c0::/29", "2c0f:f248::/32",
)

func mustPrefixes(list ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(list))
	for i, s := range list {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

// cdnOf names the proxy a hostname's addresses belong to: "cloudflare" when every one of
// them is Cloudflare's, "" otherwise. A record proxied by Cloudflare resolves to
// Cloudflare only, so one address outside its ranges means the name is not proxied.
func cdnOf(addresses []string) string {
	if len(addresses) == 0 {
		return ""
	}
	for _, a := range addresses {
		ip, err := netip.ParseAddr(a)
		if err != nil {
			return ""
		}
		ip = ip.Unmap()
		in := false
		for _, p := range cloudflareRanges {
			in = in || p.Contains(ip)
		}
		if !in {
			return ""
		}
	}
	return "cloudflare"
}
