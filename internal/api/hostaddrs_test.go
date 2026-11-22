package api

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"panel/internal/brand"
)

const fibTrie = `Main:
  +-- 0.0.0.0/0 3 0 5
     |-- 0.0.0.0
        /0 universe UNICAST
     +-- 172.17.0.0/16 2 0 2
        |-- 172.17.0.0
           /16 link UNICAST
        |-- 172.17.0.1
           /32 host LOCAL
     |-- 203.0.113.0
        /24 link UNICAST
Local:
  +-- 0.0.0.0/0 3 0 5
     +-- 127.0.0.0/8 2 0 2
        |-- 127.0.0.1
           /32 host LOCAL
        |-- 127.255.255.255
           /32 link BROADCAST
     |-- 203.0.113.5
        /32 host LOCAL
     |-- 203.0.113.255
        /32 link BROADCAST
`

const ifInet6 = `00000000000000000000000000000001 01 80 10 80       lo
fe800000000000000250569afffe0001 02 40 20 80     eth0
20010db8000000000000000000000005 02 40 00 80     eth0
fd000000000000000000000000000001 03 40 00 80  docker0
`

// In a container the panel's interfaces say nothing public: the host's addresses come
// from the mounted /proc, and only the public ones are kept.
func TestPublicAddrsFromHostProc(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "1/net"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"fib_trie": fibTrie, "if_inet6": ifInet6} {
		if err := os.WriteFile(filepath.Join(root, "1/net", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(brand.EnvPrefix()+"HOST_PROC", root)
	got := map[string]bool{}
	for _, a := range publicAddrs() {
		got[a] = true
	}
	for _, want := range []string{"203.0.113.5", "2001:db8::5"} {
		if !got[want] {
			t.Errorf("publicAddrs() = %v, want %s in it", got, want)
		}
	}
	for _, not := range []string{"172.17.0.1", "127.0.0.1", "203.0.113.255", "203.0.113.0", "fd00::1", "::1"} {
		if got[not] {
			t.Errorf("publicAddrs() has %s", not)
		}
	}
}

func TestParseFibTrieLocal(t *testing.T) {
	var got []string
	for _, ip := range parseFibTrieLocal(fibTrie) {
		got = append(got, ip.String())
	}
	if want := []string{"172.17.0.1", "127.0.0.1", "203.0.113.5"}; !reflect.DeepEqual(got, want) {
		t.Errorf("local addresses = %v, want %v", got, want)
	}
}
