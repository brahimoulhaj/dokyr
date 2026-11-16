package dbgate

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"
)

func testCert(t *testing.T) tls.Certificate {
	t.Helper()
	c, k, err := GenerateCertificate([]string{"db.example.com", "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(c, k)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// startGate runs a gate in front of backend and returns its address.
func startGate(t *testing.T, engine string, backend net.Listener, allow []netip.Prefix) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = ServeListener(ctx, l, Config{Target: backend.Addr().String(), Engine: engine, Allow: allow, Cert: testCert(t)})
		close(done)
	}()
	t.Cleanup(func() { cancel(); <-done })
	return l.Addr().String()
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestParseAllow(t *testing.T) {
	got, err := ParseAllow("203.0.113.7, 10.0.0.0/8\n2001:db8::/32")
	if err != nil || len(got) != 3 || got[0].Bits() != 32 {
		t.Fatalf("%v %v", got, err)
	}
	if !Allowed(got, netip.MustParseAddr("10.1.2.3")) || Allowed(got, netip.MustParseAddr("11.0.0.1")) || !Allowed(nil, netip.MustParseAddr("1.2.3.4")) {
		t.Fatal("allowlist matching")
	}
	if !Allowed(got, netip.MustParseAddr("::ffff:203.0.113.7")) {
		t.Fatal("IPv4-mapped address not matched")
	}
	if _, err := ParseAllow("10.0.0.0/33"); err == nil {
		t.Fatal("bad CIDR accepted")
	}
}

func TestPostgresGate(t *testing.T) {
	backend := listen(t)
	got := make(chan string, 4)
	go func() {
		for {
			c, err := backend.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 5)
				if _, err := io.ReadFull(c, buf); err != nil {
					return
				}
				got <- string(buf)
				_, _ = c.Write([]byte("reply"))
			}()
		}
	}()
	gate := startGate(t, "postgres", backend, nil)

	// TLS after SSLRequest: plaintext reaches the backend, the reply comes back.
	c, err := net.Dial("tcp", gate)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	req := make([]byte, 8)
	binary.BigEndian.PutUint32(req, 8)
	binary.BigEndian.PutUint32(req[4:], pgSSLRequest)
	c.Write(req)
	answer := make([]byte, 1)
	if _, err := io.ReadFull(c, answer); err != nil || answer[0] != 'S' {
		t.Fatalf("SSLRequest answer %q %v", answer, err)
	}
	tc := tls.Client(c, &tls.Config{InsecureSkipVerify: true})
	tc.Write([]byte("hello"))
	reply := make([]byte, 5)
	if _, err := io.ReadFull(tc, reply); err != nil || string(reply) != "reply" || <-got != "hello" {
		t.Fatalf("relay: %q %v", reply, err)
	}

	// A plain startup message is answered with an error and never forwarded.
	p, _ := net.Dial("tcp", gate)
	defer p.Close()
	startup := make([]byte, 8)
	binary.BigEndian.PutUint32(startup, 8)
	binary.BigEndian.PutUint32(startup[4:], 196608) // protocol 3.0
	p.Write(startup)
	r := bufio.NewReader(p)
	if b, _ := r.ReadByte(); b != 'E' {
		t.Fatal("plain client not refused with an ErrorResponse")
	}
	select {
	case v := <-got:
		t.Fatalf("plaintext reached the backend: %q", v)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestGateAllowlist(t *testing.T) {
	backend := listen(t)
	reached := make(chan struct{}, 1)
	go func() {
		c, err := backend.Accept()
		if err == nil {
			reached <- struct{}{}
			c.Close()
		}
	}()
	gate := startGate(t, "mariadb", backend, []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")})
	c, err := net.Dial("tcp", gate)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if n, err := c.Read(make([]byte, 1)); n != 0 || err == nil {
		t.Fatal("refused client received data")
	}
	select {
	case <-reached:
		t.Fatal("refused client reached the database")
	default:
	}
}

func greeting(caps uint16) packet {
	p := []byte{10}
	p = append(p, "11.8.0-MariaDB"...)
	p = append(p, 0, 1, 0, 0, 0) // connection id
	p = append(p, "12345678"...) // auth data part 1
	p = append(p, 0, 0, 0)       // filler + caps placeholder
	binary.LittleEndian.PutUint16(p[len(p)-2:], caps)
	p = append(p, 33, 2, 0, 0, 0, 0) // charset, status, upper caps
	return packet{seq: 0, payload: p}
}

func TestMariaDBGate(t *testing.T) {
	backend := listen(t)
	seen := make(chan packet, 2)
	go func() {
		for {
			c, err := backend.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				writePacket(c, greeting(clientProtocol)) // the database itself has no TLS
				r := bufio.NewReader(c)
				resp, err := readPacket(r)
				if err != nil {
					return
				}
				seen <- resp
				writePacket(c, packet{seq: resp.seq + 1, payload: []byte{0, 0, 0, 2, 0, 0, 0}}) // OK
				cmd, err := readPacket(r)
				if err != nil {
					return
				}
				writePacket(c, packet{seq: cmd.seq + 1, payload: append([]byte("echo:"), cmd.payload...)})
			}()
		}
	}()
	gate := startGate(t, "mariadb", backend, nil)

	c, err := net.Dial("tcp", gate)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := bufio.NewReader(c)
	g, err := readPacket(r)
	if err != nil {
		t.Fatal(err)
	}
	off, _ := greetingCapsOffset(g.payload)
	if binary.LittleEndian.Uint16(g.payload[off:])&clientSSL == 0 {
		t.Fatal("gate does not offer TLS")
	}
	ssl := make([]byte, 32)
	binary.LittleEndian.PutUint32(ssl, clientSSL|clientProtocol)
	writePacket(c, packet{seq: 1, payload: ssl})
	tc := tls.Client(c, &tls.Config{InsecureSkipVerify: true})
	hr := append(append([]byte{}, ssl...), "app\x00"...)
	writePacket(tc, packet{seq: 2, payload: hr})
	tr := bufio.NewReader(tc)
	ok, err := readPacket(tr)
	if err != nil || ok.seq != 3 || ok.payload[0] != 0 {
		t.Fatalf("OK packet: %+v %v", ok, err)
	}
	resp := <-seen
	if resp.seq != 1 || binary.LittleEndian.Uint32(resp.payload)&clientSSL != 0 {
		t.Fatalf("server saw seq %d flags %x", resp.seq, binary.LittleEndian.Uint32(resp.payload))
	}
	writePacket(tc, packet{seq: 0, payload: []byte("\x03SELECT 1")})
	echo, err := readPacket(tr)
	if err != nil || string(echo.payload) != "echo:\x03SELECT 1" || echo.seq != 1 {
		t.Fatalf("command phase: %+v %v", echo, err)
	}

	// A client that does not ask for TLS gets an error before sending credentials.
	p, _ := net.Dial("tcp", gate)
	defer p.Close()
	pr := bufio.NewReader(p)
	readPacket(pr)
	plain := make([]byte, 32)
	binary.LittleEndian.PutUint32(plain, clientProtocol)
	writePacket(p, packet{seq: 1, payload: append(plain, "app\x00"...)})
	e, err := readPacket(pr)
	if err != nil || e.payload[0] != 0xff {
		t.Fatalf("plain client: %+v %v", e, err)
	}
	select {
	case v := <-seen:
		t.Fatalf("plain handshake reached the server: %+v", v)
	case <-time.After(100 * time.Millisecond):
	}
}
