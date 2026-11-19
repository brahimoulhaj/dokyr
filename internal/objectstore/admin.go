package objectstore

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7/pkg/signer"
	"github.com/secure-io/sio-go"
	"golang.org/x/crypto/argon2"
)

// The storage's admin API is MinIO's, which RustFS implements: requests are signed like
// S3 calls, and bodies that carry credentials are encrypted with the root secret key.
// Only the few calls the panel needs are here, so the admin SDK and its dependencies
// are not pulled in.

const adminPrefix = "/minio/admin/v3"

type adminClient struct {
	endpoint  string // host:port, plain HTTP on the shared network
	accessKey string
	secretKey string
	http      *http.Client
}

// serviceAccount is the body of add-service-account: a key that acts as its parent (the
// root user) narrowed by an inline policy.
type serviceAccount struct {
	Policy      json.RawMessage `json:"policy,omitempty"`
	AccessKey   string          `json:"accessKey"`
	SecretKey   string          `json:"secretKey"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
}

func (a *adminClient) addServiceAccount(ctx context.Context, sa serviceAccount) error {
	body, err := json.Marshal(sa)
	if err != nil {
		return err
	}
	sealed, err := encryptAdmin(a.secretKey, body)
	if err != nil {
		return err
	}
	_, err = a.do(ctx, http.MethodPut, "/add-service-account", nil, sealed)
	return err
}

func (a *adminClient) deleteServiceAccount(ctx context.Context, accessKey string) error {
	_, err := a.do(ctx, http.MethodDelete, "/delete-service-account", url.Values{"accessKey": {accessKey}}, nil)
	return err
}

// adminError is an error answered by the admin API.
type adminError struct {
	Status  int
	Code    string
	Message string
}

func (e *adminError) Error() string {
	switch {
	case e.Message != "":
		return e.Message
	case e.Code != "":
		return e.Code
	}
	return fmt.Sprintf("admin API answered %d", e.Status)
}

func (a *adminClient) do(ctx context.Context, method, path string, query url.Values, body []byte) ([]byte, error) {
	u := url.URL{Scheme: "http", Host: a.endpoint, Path: adminPrefix + path, RawQuery: query.Encode()}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	req.Header.Set("X-Amz-Content-Sha256", hex.EncodeToString(sum[:]))
	req.ContentLength = int64(len(body))
	req = signer.SignV4(*req, a.accessKey, a.secretKey, "", "us-east-1")
	cl := a.http
	if cl == nil {
		cl = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		e := &adminError{Status: resp.StatusCode}
		// MinIO answers JSON, RustFS S3-style XML; take whichever parses.
		var j struct{ Code, Message string }
		if json.Unmarshal(out, &j) == nil && (j.Code != "" || j.Message != "") {
			e.Code, e.Message = j.Code, j.Message
		} else if xml.Unmarshal(out, &j) == nil {
			e.Code, e.Message = j.Code, j.Message
		} else {
			e.Message = strings.TrimSpace(string(out))
		}
		return nil, e
	}
	return out, nil
}

// encryptAdmin seals data the way madmin's EncryptData does with Argon2id and AES-GCM:
// salt (32) | algorithm id (0x00) | nonce (8) | sio stream.
func encryptAdmin(password string, data []byte) ([]byte, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	stream, err := sio.AES_256_GCM.Stream(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, stream.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.Write(salt)
	buf.WriteByte(0x00)
	buf.Write(nonce)
	w := stream.EncryptWriter(&buf, nonce, nil)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
