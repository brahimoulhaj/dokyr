package registry

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Access is what a token grants on one resource, in the registry's token format:
// {"type": "repository", "name": "shop/api", "actions": ["pull", "push"]}.
type Access struct {
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Actions []string `json:"actions"`
}

// ParseScope reads one scope parameter of a token request, "repository:shop/api:pull,push".
// The name may hold colons (a registry host with a port), so type is before the first and
// actions after the last.
func ParseScope(scope string) (Access, bool) {
	first, last := strings.Index(scope, ":"), strings.LastIndex(scope, ":")
	if first <= 0 || last <= first+1 || last == len(scope)-1 {
		return Access{}, false
	}
	return Access{Type: scope[:first], Name: scope[first+1 : last], Actions: strings.Split(scope[last+1:], ",")}, true
}

// signer signs registry tokens with an ECDSA key. Its self-signed certificate is the
// registry's rootcertbundle; tokens carry it in their x5c header.
type signer struct {
	key     *ecdsa.PrivateKey
	certPEM []byte
	certDER []byte
	issuer  string
}

// loadSigner reads the key and certificate in dir, and creates them on first use.
func loadSigner(dir, issuer string) (*signer, error) {
	keyPath, certPath := filepath.Join(dir, "token.key"), filepath.Join(dir, "token.crt")
	keyPEM, kerr := os.ReadFile(keyPath)
	certPEM, cerr := os.ReadFile(certPath)
	if kerr == nil && cerr == nil {
		kb, _ := pem.Decode(keyPEM)
		cb, _ := pem.Decode(certPEM)
		if kb == nil || cb == nil {
			return nil, errors.New("registry: token key or certificate is not PEM")
		}
		key, err := x509.ParseECPrivateKey(kb.Bytes)
		if err != nil {
			return nil, fmt.Errorf("registry: token key: %w", err)
		}
		return &signer{key: key, certPEM: certPEM, certDER: cb.Bytes, issuer: issuer}, nil
	}
	if !errors.Is(kerr, os.ErrNotExist) && kerr != nil {
		return nil, kerr
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: issuer + " registry token signer"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(30, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	kder, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder}), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return nil, err
	}
	return &signer{key: key, certPEM: certPEM, certDER: der, issuer: issuer}, nil
}

// issue returns a signed JWT for subject on service (the registry's domain) granting access.
func (s *signer) issue(service, subject string, access []Access, ttl time.Duration) (string, error) {
	if access == nil {
		access = []Access{}
	}
	now := time.Now()
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", err
	}
	header := map[string]any{"typ": "JWT", "alg": "ES256", "x5c": []string{base64.StdEncoding.EncodeToString(s.certDER)}}
	claims := map[string]any{
		"iss": s.issuer, "sub": subject, "aud": service,
		"iat": now.Unix(), "nbf": now.Add(-10 * time.Second).Unix(), "exp": now.Add(ttl).Unix(),
		"jti": base64.RawURLEncoding.EncodeToString(jti), "access": access,
	}
	h, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	c, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(c)
	sum := sha256.Sum256([]byte(signing))
	r, sg, err := ecdsa.Sign(rand.Reader, s.key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64) // ES256: r and s, 32 bytes each, big-endian
	r.FillBytes(sig[:32])
	sg.FillBytes(sig[32:])
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
