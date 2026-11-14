package api

import (
	"context"
	"errors"
	"testing"

	"panel/internal/store"
)

func TestS3ConnectionsAPI(t *testing.T) {
	e := newEnv(t)
	var checked []store.S3Input
	e.srv.SetS3Tester(func(_ context.Context, c store.S3Input) error {
		checked = append(checked, c)
		if c.SecretKey != "good" {
			return errors.New("the secret key does not match the access key")
		}
		return nil
	})
	if code, _ := e.call("GET", "/api/s3-connections", nil); code != 401 {
		t.Fatalf("signed out = %d", code)
	}
	e.signIn()

	ok := map[string]any{"endpoint": "https://S3.example.com/", "bucket": "images", "prefix": "/panel/", "access_key": "AK", "secret_key": "good"}
	for _, bad := range []map[string]any{
		{"endpoint": "s3.example.com/x", "bucket": "images", "access_key": "AK", "secret_key": "good"},
		{"endpoint": "s3.example.com", "bucket": "I", "access_key": "AK", "secret_key": "good"},
		{"endpoint": "s3.example.com", "bucket": "images", "access_key": "", "secret_key": "good"},
		{"endpoint": "s3.example.com", "bucket": "images", "access_key": "AK", "secret_key": ""},
		{"endpoint": "s3.example.com", "bucket": "images", "prefix": "../up", "access_key": "AK", "secret_key": "good"},
	} {
		e.must(422, "POST", "/api/s3-connections", bad)
	}
	e.must(502, "POST", "/api/s3-connections", map[string]any{"endpoint": "s3.example.com", "bucket": "images", "access_key": "AK", "secret_key": "bad"})

	c := e.must(201, "POST", "/api/s3-connections", ok)
	if c["endpoint"] != "s3.example.com" || c["use_ssl"] != true || c["prefix"] != "panel" || c["name"] != "images" || c["secret_key"] != nil {
		t.Fatalf("connection = %v", c)
	}
	plain := e.must(201, "POST", "/api/s3-connections", map[string]any{"name": "Local", "endpoint": "http://minio:9000", "bucket": "b1b", "access_key": "AK", "secret_key": "good", "path_style": true})
	if plain["use_ssl"] != false || plain["path_style"] != true {
		t.Errorf("http endpoint = %v", plain)
	}
	if list := e.list("/api/s3-connections"); len(list) != 2 {
		t.Fatalf("list = %v", list)
	}

	// The secret is stored encrypted and comes back for the check.
	id := c["id"].(string)
	e.must(200, "POST", "/api/s3-connections/"+id+"/test", nil)
	if last := checked[len(checked)-1]; last.SecretKey != "good" || last.Bucket != "images" {
		t.Errorf("test got %+v", last)
	}

	// In use by the registry: refused.
	if err := e.srv.store.SetSetting(context.Background(), store.SettingRegistryS3, id); err != nil {
		t.Fatal(err)
	}
	e.must(409, "DELETE", "/api/s3-connections/"+id, nil)
	if err := e.srv.store.DeleteSetting(context.Background(), store.SettingRegistryS3); err != nil {
		t.Fatal(err)
	}
	e.must(204, "DELETE", "/api/s3-connections/"+id, nil)
	e.must(404, "DELETE", "/api/s3-connections/"+id, nil)
}
