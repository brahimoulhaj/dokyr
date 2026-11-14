package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"panel/internal/executor"
	"panel/internal/store"
)

func base64URL(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }

var client = &http.Client{Timeout: 30 * time.Second}

// call makes one request to the registry's API with a token of the panel's account.
func (s *Service) call(ctx context.Context, method, p string, accept string, access ...Access) (*http.Response, error) {
	tok, err := s.internalToken(ctx, access...)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL()+p, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	return resp, nil
}

func decode(resp *http.Response, v any) error {
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return store.ErrNotFound
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("registry: %s: %s", resp.Status, b)
	}
	if v == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// Repositories lists the repositories in the registry (up to 1000).
func (s *Service) Repositories(ctx context.Context) ([]string, error) {
	resp, err := s.call(ctx, "GET", "/v2/_catalog?n=1000", "", Access{Type: "registry", Name: "catalog", Actions: []string{"*"}})
	if err != nil {
		return nil, err
	}
	var out struct {
		Repositories []string `json:"repositories"`
	}
	if err := decode(resp, &out); err != nil {
		return nil, err
	}
	if out.Repositories == nil {
		out.Repositories = []string{}
	}
	return out.Repositories, nil
}

// Tags lists a repository's tags; none for a repository that is gone.
func (s *Service) Tags(ctx context.Context, repo string) ([]string, error) {
	resp, err := s.call(ctx, "GET", "/v2/"+repo+"/tags/list?n=1000", "", Access{Type: "repository", Name: repo, Actions: []string{"pull"}})
	if err != nil {
		return nil, err
	}
	var out struct {
		Tags []string `json:"tags"`
	}
	if err := decode(resp, &out); err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	return out.Tags, nil
}

const manifestTypes = "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json"

// DeleteTag removes a tag: the registry deletes by digest, so the tag's manifest goes
// (and any other tag on the same digest). Space comes back after garbage collection.
func (s *Service) DeleteTag(ctx context.Context, repo, tag string) error {
	access := Access{Type: "repository", Name: repo, Actions: []string{"pull", "delete"}}
	resp, err := s.call(ctx, "HEAD", "/v2/"+repo+"/manifests/"+url.PathEscape(tag), manifestTypes, access)
	if err != nil {
		return err
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	if err := decode(resp, nil); err != nil {
		return err
	}
	if digest == "" {
		return errors.New("registry: no digest for the tag")
	}
	resp, err = s.call(ctx, "DELETE", "/v2/"+repo+"/manifests/"+digest, "", access)
	if err != nil {
		return err
	}
	return decode(resp, nil)
}

// nopExecutor stands in where no container is run (unit tests).
type nopExecutor struct{ executor.Executor }
