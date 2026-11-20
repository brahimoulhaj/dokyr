package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"panel/internal/brand"
)

// apiClient speaks the panel's JSON API. token is empty before sign-in.
type apiClient struct {
	base  string // scheme and host, no trailing slash
	token string
	http  *http.Client
}

func newClient(base, token string) *apiClient {
	return &apiClient{base: base, token: token, http: &http.Client{
		Timeout: 30 * time.Second,
		// A panel answers its API itself. A redirect would carry the token elsewhere.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// apiError is an error answer of the panel: {"error": {"code", "message"}}.
type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string { return e.Message }

// do sends a request and decodes a 2xx answer into out (nil to ignore it).
func (c *apiClient) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", brand.Slug+"-cli/"+version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("cannot reach %s: %w", c.base, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("reading the answer of %s: %w", c.base, err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		var e struct {
			Error struct{ Code, Message string }
		}
		if json.Unmarshal(raw, &e) != nil || e.Error.Message == "" {
			return &apiError{Status: res.StatusCode, Message: fmt.Sprintf("%s answered %s; is it a %s panel?", c.base, res.Status, brand.Name)}
		}
		return &apiError{Status: res.StatusCode, Code: e.Error.Code, Message: e.Error.Message}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s did not answer with JSON; is it a %s panel?", c.base, brand.Name)
	}
	return nil
}

// signedOut reports whether the panel refused the token itself (revoked, or its user gone).
func signedOut(err error) bool {
	var e *apiError
	return errors.As(err, &e) && e.Status == http.StatusUnauthorized
}
