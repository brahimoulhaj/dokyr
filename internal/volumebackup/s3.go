package volumebackup

import (
	"context"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"

	"panel/internal/storage"
	"panel/internal/store"
)

type s3Objects struct {
	client *minio.Client
	bucket string
}

// OpenS3 returns the Open function of a Service: the bucket of an S3 connection, and
// the folder prefix that connection keeps its objects under.
func OpenS3(st *store.Store) func(ctx context.Context, connectionID string) (Objects, string, error) {
	return func(ctx context.Context, connectionID string) (Objects, string, error) {
		c, err := st.S3Secret(ctx, connectionID)
		if err != nil {
			return nil, "", err
		}
		client, err := storage.Client(c)
		if err != nil {
			return nil, "", err
		}
		return s3Objects{client, c.Bucket}, strings.Trim(c.Prefix, "/"), nil
	}
}

func (o s3Objects) Put(ctx context.Context, key string, r io.Reader) (int64, error) {
	info, err := o.client.PutObject(ctx, o.bucket, key, r, -1, minio.PutObjectOptions{ContentType: "application/gzip"})
	return info.Size, err
}

func (o s3Objects) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return o.client.GetObject(ctx, o.bucket, key, minio.GetObjectOptions{})
}

func (o s3Objects) Delete(ctx context.Context, key string) error {
	return o.client.RemoveObject(ctx, o.bucket, key, minio.RemoveObjectOptions{})
}
