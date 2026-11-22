package api

import "testing"

func TestCDNOf(t *testing.T) {
	for _, c := range []struct {
		addrs []string
		want  string
	}{
		{[]string{"104.21.12.100", "172.67.152.21", "2606:4700:3034::6815:c64"}, "cloudflare"},
		{[]string{"173.249.55.149"}, ""},
		// 173.245.48.0/20 is Cloudflare's, 173.249.x is not: the prefix decides, not the look.
		{[]string{"173.245.48.1"}, "cloudflare"},
		{[]string{"104.21.12.100", "203.0.113.5"}, ""},
		{[]string{"2c0f:fa18:0:10::adf9:3795"}, ""},
		{[]string{"not-an-address"}, ""},
		{nil, ""},
	} {
		if got := cdnOf(c.addrs); got != c.want {
			t.Errorf("cdnOf(%v) = %q, want %q", c.addrs, got, c.want)
		}
	}
}
