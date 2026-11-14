// Package storage talks to S3-compatible object storage.
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"panel/internal/id"
	"panel/internal/store"
)

// Client opens a client for a connection.
func Client(c store.S3Input) (*minio.Client, error) {
	lookup := minio.BucketLookupDNS
	if c.PathStyle {
		lookup = minio.BucketLookupPath
	}
	return minio.New(c.Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""),
		Secure:       c.UseSSL,
		Region:       c.Region,
		BucketLookup: lookup,
	})
}

// Test checks that the bucket exists and that the keys can write, read and delete in it,
// with a small object under the prefix that it removes again.
func Test(ctx context.Context, c store.S3Input) error {
	cl, err := Client(c)
	if err != nil {
		return err
	}
	ok, err := cl.BucketExists(ctx, c.Bucket)
	if err != nil {
		return explain(err)
	}
	if !ok {
		return fmt.Errorf("bucket %q does not exist", c.Bucket)
	}
	key := path.Join(strings.Trim(c.Prefix, "/"), ".panel-check-"+strings.ToLower(id.New()))
	body := []byte("check")
	if _, err := cl.PutObject(ctx, c.Bucket, key, bytes.NewReader(body), int64(len(body)), minio.PutObjectOptions{}); err != nil {
		return fmt.Errorf("write: %w", explain(err))
	}
	obj, err := cl.GetObject(ctx, c.Bucket, key, minio.GetObjectOptions{})
	if err == nil {
		_, err = obj.Stat()
		obj.Close()
	}
	if err != nil {
		return fmt.Errorf("read: %w", explain(err))
	}
	if err := cl.RemoveObject(ctx, c.Bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete: %w", explain(err))
	}
	return nil
}

// explain turns the S3 error codes people meet most into plain words.
func explain(err error) error {
	var r minio.ErrorResponse
	if !errors.As(err, &r) {
		return err
	}
	switch r.Code {
	case "InvalidAccessKeyId":
		return errors.New("the access key is not known to this endpoint")
	case "SignatureDoesNotMatch":
		return errors.New("the secret key does not match the access key")
	case "AccessDenied":
		return errors.New("access denied: the keys lack permission on this bucket")
	case "NoSuchBucket":
		return errors.New("the bucket does not exist")
	}
	return err
}
