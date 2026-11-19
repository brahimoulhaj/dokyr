// Package dbgate is the public entrance of a database instance. It runs in its own
// container, publishes the chosen host port, drops clients outside the allowlist,
// and terminates TLS itself, so the database container stays internal and unchanged:
// clients that do not start TLS are refused before they can send a password.
package dbgate

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Config describes one gate. An empty Allow admits every address.
type Config struct {
	Listen string // e.g. ":5432"
	Target string // the database on the private network, e.g. "dokyr-db-x:5432"
	Engine string // "postgres" or "mariadb"
	Allow  []netip.Prefix
	Cert   tls.Certificate
	Log    *slog.Logger

	// established is called once a client has finished TLS: it frees its handshake slot.
	established func()
}

// maxHandshakes is how many connections may be in the unauthenticated phase at once. A
// client in that phase costs a goroutine and, for MariaDB, a connection to the database:
// without a cap, anyone who can reach the port could use up the database's own limit.
const maxHandshakes = 64

// handshakeTimeout bounds the unauthenticated phase, so idle sockets cannot pile up.
const handshakeTimeout = 15 * time.Second

// ParseAllow reads comma- or newline-separated IPs and CIDRs.
func ParseAllow(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' || r == '\r' || r == '\t' }) {
		if !strings.Contains(f, "/") {
			a, err := netip.ParseAddr(f)
			if err != nil {
				return nil, fmt.Errorf("%q is not an IP address or CIDR range", f)
			}
			out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(f)
		if err != nil {
			return nil, fmt.Errorf("%q is not an IP address or CIDR range", f)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

// Allowed reports whether addr may connect.
func Allowed(allow []netip.Prefix, addr netip.Addr) bool {
	if len(allow) == 0 {
		return true
	}
	addr = addr.Unmap()
	for _, p := range allow {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// Serve accepts connections until ctx ends.
func Serve(ctx context.Context, c Config) error {
	if c.Engine != "postgres" && c.Engine != "mariadb" {
		return fmt.Errorf("unknown engine %q", c.Engine)
	}
	l, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return err
	}
	return ServeListener(ctx, l, c)
}

// ServeListener is Serve on an existing listener (tests use a random port).
func ServeListener(ctx context.Context, l net.Listener, c Config) error {
	if c.Log == nil {
		c.Log = slog.New(slog.DiscardHandler)
	}
	go func() { <-ctx.Done(); l.Close() }()
	var wg sync.WaitGroup
	defer wg.Wait()
	slots := make(chan struct{}, maxHandshakes)
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		select {
		case slots <- struct{}{}:
		default:
			c.Log.Info("refused: too many connections in handshake", "remote", conn.RemoteAddr().String())
			conn.Close()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			cc := c
			var once sync.Once
			cc.established = func() { once.Do(func() { <-slots }) }
			defer cc.established()
			cc.handle(ctx, conn)
		}()
	}
}

func (c Config) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	remote, _ := netip.ParseAddrPort(conn.RemoteAddr().String())
	if !Allowed(c.Allow, remote.Addr()) {
		c.Log.Info("refused: address not allowed", "remote", remote.Addr().String())
		return
	}
	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))
	var err error
	if c.Engine == "postgres" {
		err = c.postgres(ctx, conn)
	} else {
		err = c.mariadb(ctx, conn)
	}
	if err != nil {
		c.Log.Info("connection closed", "remote", remote.Addr().String(), "reason", err.Error())
	}
}

func (c Config) dial(ctx context.Context) (net.Conn, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	return d.DialContext(ctx, "tcp", c.Target)
}

func (c Config) tlsConfig(alpn ...string) *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{c.Cert}, MinVersion: tls.VersionTLS12, NextProtos: alpn}
}

// pipe copies both ways until either side closes.
func pipe(a, b net.Conn) {
	_ = a.SetDeadline(time.Time{})
	_ = b.SetDeadline(time.Time{})
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.Close()
		}
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	<-done
}

// PostgreSQL request codes sent as the first message, before any startup packet.
const (
	pgSSLRequest    = 80877103
	pgGSSENCRequest = 80877104
	pgCancelRequest = 80877102
)

func (c Config) postgres(ctx context.Context, conn net.Conn) error {
	r := bufio.NewReader(conn)
	first, err := r.Peek(1)
	if err != nil {
		return err
	}
	var client net.Conn
	if first[0] == 0x16 {
		// PostgreSQL 17 "direct" TLS: a TLS ClientHello without SSLRequest.
		client = tls.Server(&bufferedConn{conn, r}, c.tlsConfig("postgresql"))
	} else {
		for {
			var head [8]byte
			if _, err := io.ReadFull(r, head[:]); err != nil {
				return err
			}
			size, code := binary.BigEndian.Uint32(head[:4]), binary.BigEndian.Uint32(head[4:])
			switch {
			case size == 8 && code == pgGSSENCRequest:
				// No GSSAPI encryption here; libpq then falls back to SSLRequest.
				if _, err := conn.Write([]byte{'N'}); err != nil {
					return err
				}
				continue
			case size == 16 && code == pgCancelRequest:
				// A cancel request carries only a process key and cannot use TLS on older
				// servers; forward it as is.
				rest := make([]byte, 8)
				if _, err := io.ReadFull(r, rest); err != nil {
					return err
				}
				backend, err := c.dial(ctx)
				if err != nil {
					return err
				}
				defer backend.Close()
				_, err = backend.Write(append(head[:], rest...))
				return err
			case size == 8 && code == pgSSLRequest:
				if _, err := conn.Write([]byte{'S'}); err != nil {
					return err
				}
				client = tls.Server(&bufferedConn{conn, r}, c.tlsConfig())
			default:
				// A plain startup message: answer with a PostgreSQL error, never forward.
				_, _ = conn.Write(pgError("TLS is required for public connections; use sslmode=require"))
				return errors.New("client did not request TLS")
			}
			break
		}
	}
	if err := client.(*tls.Conn).HandshakeContext(ctx); err != nil {
		return err
	}
	backend, err := c.dial(ctx)
	if err != nil {
		_, _ = client.Write(pgError("the database is not reachable"))
		return err
	}
	defer backend.Close()
	c.established()
	pipe(client, backend)
	return nil
}

// pgError builds an ErrorResponse message with severity FATAL.
func pgError(msg string) []byte {
	body := []byte{}
	for _, f := range [][2]string{{"S", "FATAL"}, {"V", "FATAL"}, {"C", "08004"}, {"M", msg}} {
		body = append(body, f[0][0])
		body = append(body, f[1]...)
		body = append(body, 0)
	}
	body = append(body, 0)
	out := []byte{'E', 0, 0, 0, 0}
	binary.BigEndian.PutUint32(out[1:], uint32(len(body)+4))
	return append(out, body...)
}

// MariaDB/MySQL protocol details used during the connection phase.
const (
	clientSSL      = 0x00000800
	clientProtocol = 0x00000200 // CLIENT_PROTOCOL_41
)

type packet struct {
	seq     byte
	payload []byte
}

func readPacket(r io.Reader) (packet, error) {
	var h [4]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return packet{}, err
	}
	n := int(h[0]) | int(h[1])<<8 | int(h[2])<<16
	if n >= 1<<24-1 {
		return packet{}, errors.New("oversized handshake packet")
	}
	p := packet{seq: h[3], payload: make([]byte, n)}
	_, err := io.ReadFull(r, p.payload)
	return p, err
}

func writePacket(w io.Writer, p packet) error {
	n := len(p.payload)
	_, err := w.Write(append([]byte{byte(n), byte(n >> 8), byte(n >> 16), p.seq}, p.payload...))
	return err
}

func myError(seq byte, msg string) packet {
	// ERR packet: 0xff, code 1045 (access denied), '#', SQLSTATE, message.
	return packet{seq: seq, payload: append([]byte{0xff, 0x15, 0x04, '#', '2', '8', '0', '0', '0'}, msg...)}
}

// greetingCapsOffset finds the lower capability flags in a protocol-10 greeting.
func greetingCapsOffset(p []byte) (int, error) {
	if len(p) < 1 || p[0] != 10 {
		return 0, errors.New("unexpected server greeting")
	}
	end := 1
	for end < len(p) && p[end] != 0 {
		end++
	}
	off := end + 1 + 4 + 8 + 1 // version\0, connection id, auth data part 1, filler
	if off+2 > len(p) {
		return 0, errors.New("short server greeting")
	}
	return off, nil
}

func (c Config) mariadb(ctx context.Context, conn net.Conn) error {
	backend, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer backend.Close()
	_ = backend.SetDeadline(time.Now().Add(handshakeTimeout))
	br := bufio.NewReader(backend)
	greeting, err := readPacket(br)
	if err != nil {
		return err
	}
	if len(greeting.payload) > 0 && greeting.payload[0] == 0xff {
		return writePacket(conn, greeting) // e.g. too many connections
	}
	off, err := greetingCapsOffset(greeting.payload)
	if err != nil {
		return err
	}
	// The gate offers TLS on the server's behalf.
	caps := binary.LittleEndian.Uint16(greeting.payload[off:])
	binary.LittleEndian.PutUint16(greeting.payload[off:], caps|clientSSL)
	if err := writePacket(conn, greeting); err != nil {
		return err
	}
	cr := bufio.NewReader(conn)
	req, err := readPacket(cr)
	if err != nil {
		return err
	}
	if len(req.payload) < 4 || binary.LittleEndian.Uint32(req.payload)&clientSSL == 0 {
		_ = writePacket(conn, myError(req.seq+1, "TLS is required for public connections; connect with --ssl"))
		return errors.New("client did not request TLS")
	}
	client := tls.Server(&bufferedConn{conn, cr}, c.tlsConfig())
	if err := client.HandshakeContext(ctx); err != nil {
		return err
	}
	tr := bufio.NewReader(client)
	resp, err := readPacket(tr)
	if err != nil {
		return err
	}
	if len(resp.payload) < 4 {
		return errors.New("short handshake response")
	}
	// The server never saw the SSL request: clear the flag, and shift sequence ids by
	// one in both directions until authentication ends.
	flags := binary.LittleEndian.Uint32(resp.payload) &^ clientSSL
	binary.LittleEndian.PutUint32(resp.payload, flags)
	resp.seq--
	if err := writePacket(backend, resp); err != nil {
		return err
	}
	for {
		p, err := readPacket(br)
		if err != nil {
			return err
		}
		p.seq++
		if err := writePacket(client, p); err != nil {
			return err
		}
		if len(p.payload) == 0 {
			continue
		}
		switch p.payload[0] {
		case 0x00, 0xff: // OK or ERR: authentication is over
			if p.payload[0] == 0xff {
				return errors.New("authentication failed")
			}
			// Bytes already buffered from either side must not be lost.
			c.established()
			pipe(&bufferedConn{client, tr}, &bufferedConn{backend, br})
			return nil
		}
		if len(p.payload) == 2 && p.payload[0] == 0x01 && p.payload[1] == 0x03 {
			continue // caching_sha2 fast-auth success: the OK packet follows at once
		}
		// Auth switch (0xfe) or more data (0x01): the client answers next.
		q, err := readPacket(tr)
		if err != nil {
			return err
		}
		q.seq--
		if err := writePacket(backend, q); err != nil {
			return err
		}
	}
}

// bufferedConn reads through a bufio.Reader that may already hold peeked bytes.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.r.Read(p) }

func (b *bufferedConn) CloseWrite() error {
	if cw, ok := b.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return b.Conn.Close()
}
