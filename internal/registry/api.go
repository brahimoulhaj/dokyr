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

// TagDetail describes what a tag points to.
type TagDetail struct {
	Digest    string   // of the tag's manifest (or index)
	Size      int64    // compressed layers and configs of its images, as stored
	Platforms []string // "linux/amd64", …; none for a single image without platform info
	// Blobs are the layers and configs it uses, by digest, with their size: tags often
	// share layers, so a repository's size counts each blob once.
	Blobs map[string]int64
}

// manifest covers an image manifest and an image index (manifest list).
type manifest struct {
	MediaType string `json:"mediaType"`
	Config    blob   `json:"config"`
	Layers    []blob `json:"layers"`
	Manifests []struct {
		Digest   string `json:"digest"`
		Platform *struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
			Variant      string `json:"variant"`
		} `json:"platform"`
	} `json:"manifests"`
}

type blob struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// addBlobs records the image's blobs and returns its size.
func (m manifest) addBlobs(into map[string]int64) int64 {
	n := m.Config.Size
	into[m.Config.Digest] = m.Config.Size
	for _, l := range m.Layers {
		n += l.Size
		into[l.Digest] = l.Size
	}
	return n
}

func (s *Service) manifest(ctx context.Context, repo, ref string) (manifest, string, error) {
	resp, err := s.call(ctx, "GET", "/v2/"+repo+"/manifests/"+url.PathEscape(ref), manifestTypes,
		Access{Type: "repository", Name: repo, Actions: []string{"pull"}})
	if err != nil {
		return manifest{}, "", err
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	var m manifest
	if err := decode(resp, &m); err != nil {
		return manifest{}, "", err
	}
	return m, digest, nil
}

// TagDetail reads a tag's manifest: its digest, its size, and the platforms it runs on.
// For an index, each platform's image counts; attestations (platform unknown/unknown,
// as buildx adds) are left out.
func (s *Service) TagDetail(ctx context.Context, repo, tag string) (TagDetail, error) {
	m, digest, err := s.manifest(ctx, repo, tag)
	if err != nil {
		return TagDetail{}, err
	}
	d := TagDetail{Digest: digest, Platforms: []string{}, Blobs: map[string]int64{}}
	if len(m.Manifests) == 0 {
		d.Size = m.addBlobs(d.Blobs)
		// A single image keeps its platform in its config blob, not in the manifest.
		if p := s.configPlatform(ctx, repo, m.Config.Digest); p != "" {
			d.Platforms = append(d.Platforms, p)
		}
		return d, nil
	}
	for _, child := range m.Manifests {
		if p := child.Platform; p != nil && p.OS == "unknown" {
			continue
		}
		cm, _, err := s.manifest(ctx, repo, child.Digest)
		if err != nil {
			return TagDetail{}, err
		}
		d.Size += cm.addBlobs(d.Blobs)
		if p := child.Platform; p != nil {
			name := p.OS + "/" + p.Architecture
			if p.Variant != "" {
				name += "/" + p.Variant
			}
			d.Platforms = append(d.Platforms, name)
		}
	}
	return d, nil
}

// configPlatform reads os/architecture[/variant] from an image's config blob; "" when it
// cannot be read (a missing or non-image config).
func (s *Service) configPlatform(ctx context.Context, repo, digest string) string {
	if digest == "" {
		return ""
	}
	resp, err := s.call(ctx, "GET", "/v2/"+repo+"/blobs/"+digest, "", Access{Type: "repository", Name: repo, Actions: []string{"pull"}})
	if err != nil {
		return ""
	}
	var c struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		Variant      string `json:"variant"`
	}
	if decode(resp, &c) != nil || c.OS == "" || c.Architecture == "" {
		return ""
	}
	if c.Variant != "" {
		return c.OS + "/" + c.Architecture + "/" + c.Variant
	}
	return c.OS + "/" + c.Architecture
}

// nopExecutor stands in where no container is run (unit tests).
type nopExecutor struct{ executor.Executor }
