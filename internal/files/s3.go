package files

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/jniltinho/meethub/internal/config"
)

// s3PartSize is the buffer of a streamed upload of unknown size; without it
// the client sizes its parts for the largest possible object (hundreds of MiB).
const s3PartSize = 16 << 20

// S3 stores objects in a bucket of an S3-compatible service (RustFS, MinIO, AWS).
type S3 struct {
	client *minio.Client
	bucket string
}

// NewS3 builds the bucket storage; it does not connect (Check does).
func NewS3(cfg config.FilesS3T) (*S3, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
		// fail within seconds while the service is down (the default retries for half a minute)
		MaxRetries: 3,
	})
	if err != nil {
		return nil, fmt.Errorf("files.s3: %w", err)
	}
	return &S3{client: client, bucket: cfg.Bucket}, nil
}

// Put streams the object; an interrupted upload is aborted by the client.
func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	opts := minio.PutObjectOptions{}
	if size < 0 {
		opts.PartSize = s3PartSize
	}
	lr := &lastErrReader{r: r}
	if _, err := s.client.PutObject(ctx, s.bucket, key, lr, size, opts); err != nil {
		if lr.err != nil && !errors.Is(lr.err, io.EOF) {
			return lr.err // the reader's own error (a size limit), not the storage's
		}
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return nil
}

// Open returns the object; it is fetched as it is read.
func (s *S3) Open(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if _, err := obj.Stat(); err != nil { // GetObject is lazy: fail here, not in the middle of a response
		_ = obj.Close()
		if minio.ToErrorResponse(err).Code == minio.NoSuchKey {
			return nil, ErrNoObject
		}
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return obj, nil
}

// Delete removes the object (S3 answers success for a missing key).
func (s *S3) Delete(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return nil
}

// Check proves the bucket exists and answers.
func (s *S3) Check(ctx context.Context) error {
	if ok, err := s.client.BucketExists(ctx, s.bucket); err != nil || !ok {
		return ErrUnavailable
	}
	return nil
}

// NewStorage builds the configured driver.
func NewStorage(cfg *config.Config) (Storage, error) {
	if cfg.Files.Driver == config.FilesS3 {
		return NewS3(cfg.Files.S3)
	}
	return NewLocal(cfg.FilesDir())
}
