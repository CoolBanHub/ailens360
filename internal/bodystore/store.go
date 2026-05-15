// Package bodystore is the S3-compatible blob store used to persist large
// request / response bodies outside of Postgres. The proxy uploads on the hot
// path; the api process generates presigned GET URLs for the UI.
//
// Object key convention (callers must follow): "{project_id}/{YYYYMM}/{trace_id}/{request|response}.{ext}".
// The bucket layout is project-scoped, time-bucketed, and one prefix per trace
// so lifecycle / archive policies can target months or projects directly.
package bodystore

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Store is the surface the rest of the code talks to. Implementations are
// concurrency-safe; a single instance is shared across the process.
type Store interface {
	// UploadBytes writes a fully-buffered payload (typical for request bodies
	// and small non-stream responses). Returns the bytes uploaded (post-gzip if
	// enabled, otherwise the input size).
	UploadBytes(ctx context.Context, key string, data []byte, contentType string) (int64, error)

	// NewStreamingUploader returns a writer that pipes into a multipart upload.
	// Writes are forwarded byte-for-byte to S3 (optionally gzip-compressed).
	// Slow object storage backpressures into Write — callers that cannot tolerate
	// that should buffer upstream.
	NewStreamingUploader(ctx context.Context, key, contentType string) (StreamingUploader, error)

	// Get opens the object for reading and returns its content metadata.
	// Callers MUST Close the reader. ContentEncoding ("gzip" when GzipBodies
	// was on at upload time) needs to be propagated to the browser as-is so
	// the browser's decoder handles decompression.
	Get(ctx context.Context, key string) (io.ReadCloser, ObjectMeta, error)

	// PresignGet returns a time-bounded GET URL — used when the api is
	// configured to redirect the browser to MinIO/S3 directly instead of
	// proxying bytes.
	PresignGet(ctx context.Context, key string) (string, error)

	// EnsureBucket creates the bucket if it doesn't exist. Called once at process
	// startup. Idempotent.
	EnsureBucket(ctx context.Context) error
}

// ObjectMeta is the small subset of object headers callers need when streaming
// bytes back to the browser.
type ObjectMeta struct {
	ContentType     string
	ContentEncoding string
	Size            int64
}

// StreamingUploader is the writer side of an in-flight multipart upload. Close
// blocks until the upload completes (or errors).
type StreamingUploader interface {
	io.WriteCloser
	// BytesWritten reports the uncompressed byte count that has been Write()n
	// so far. Useful for the size metadata persisted alongside the object key.
	BytesWritten() int64
}

// Config is what the loader builds from env vars.
type Config struct {
	Endpoint        string
	Bucket          string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	PathStyle       bool
	PartSize        int64
	GzipBodies      bool
	UploadTimeout   time.Duration
	PresignTTL      time.Duration
	PublicEndpoint  string
}

type minioStore struct {
	cfg        Config
	cli        *minio.Client
	presignCli *minio.Client // may differ from cli if PublicEndpoint is set
}

// New constructs a Store backed by minio-go. The client validates connectivity
// lazily — explicit EnsureBucket is the recommended startup probe.
func New(cfg Config) (Store, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("bodystore: endpoint required")
	}
	if cfg.Bucket == "" {
		return nil, errors.New("bodystore: bucket required")
	}
	if cfg.PartSize < 5<<20 {
		return nil, errors.New("bodystore: part_size must be >= 5 MiB")
	}
	cli, err := newClient(cfg.Endpoint, cfg)
	if err != nil {
		return nil, err
	}
	presignCli := cli
	if cfg.PublicEndpoint != "" && cfg.PublicEndpoint != cfg.Endpoint {
		presignCli, err = newClient(cfg.PublicEndpoint, cfg)
		if err != nil {
			return nil, fmt.Errorf("bodystore: public endpoint: %w", err)
		}
	}
	return &minioStore{cfg: cfg, cli: cli, presignCli: presignCli}, nil
}

func newClient(endpoint string, cfg Config) (*minio.Client, error) {
	opts := &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure:       cfg.UseSSL,
		Region:       cfg.Region,
		BucketLookup: minio.BucketLookupAuto,
	}
	if cfg.PathStyle {
		opts.BucketLookup = minio.BucketLookupPath
	}
	return minio.New(endpoint, opts)
}

func (s *minioStore) EnsureBucket(ctx context.Context) error {
	exists, err := s.cli.BucketExists(ctx, s.cfg.Bucket)
	if err != nil {
		return fmt.Errorf("bodystore: head bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := s.cli.MakeBucket(ctx, s.cfg.Bucket, minio.MakeBucketOptions{Region: s.cfg.Region}); err != nil {
		// Race: another process may have created it between the BucketExists
		// check and MakeBucket. Re-check; swallow only if it's now present.
		if exists2, _ := s.cli.BucketExists(ctx, s.cfg.Bucket); exists2 {
			return nil
		}
		return fmt.Errorf("bodystore: make bucket: %w", err)
	}
	return nil
}

func (s *minioStore) UploadBytes(ctx context.Context, key string, data []byte, contentType string) (int64, error) {
	if s.cfg.UploadTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.cfg.UploadTimeout)
		defer cancel()
	}
	u, err := s.NewStreamingUploader(ctx, key, contentType)
	if err != nil {
		return 0, err
	}
	if _, err := u.Write(data); err != nil {
		_ = u.Close()
		return 0, err
	}
	if err := u.Close(); err != nil {
		return 0, err
	}
	return u.BytesWritten(), nil
}

func (s *minioStore) NewStreamingUploader(ctx context.Context, key, contentType string) (StreamingUploader, error) {
	pr, pw := io.Pipe()
	u := &pipeUploader{pw: pw, done: make(chan error, 1)}

	opts := minio.PutObjectOptions{
		ContentType: contentType,
		PartSize:    uint64(s.cfg.PartSize),
		// DisableContentSha256 avoids a second pass over a streaming reader
		// just to compute SHA256 — MinIO and modern S3 both accept it.
		DisableContentSha256: true,
	}
	if s.cfg.GzipBodies {
		opts.ContentEncoding = "gzip"
		u.gz = gzip.NewWriter(pw)
	}

	// Upload runs in a goroutine; Write pumps into the pipe, the SDK consumes
	// it as a multipart upload (size=-1 → unknown length).
	go func() {
		_, err := s.cli.PutObject(ctx, s.cfg.Bucket, key, pr, -1, opts)
		// CloseWithError unblocks the writer side if the upload aborted early.
		_ = pr.CloseWithError(err)
		u.done <- err
	}()

	return u, nil
}

func (s *minioStore) Get(ctx context.Context, key string) (io.ReadCloser, ObjectMeta, error) {
	obj, err := s.cli.GetObject(ctx, s.cfg.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, ObjectMeta{}, err
	}
	// Stat to surface the canonical content metadata. minio-go defers the HEAD
	// request, so this is also how we discover a 404 — the error surfaces here.
	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		return nil, ObjectMeta{}, err
	}
	return obj, ObjectMeta{
		ContentType:     info.ContentType,
		ContentEncoding: info.Metadata.Get("Content-Encoding"),
		Size:            info.Size,
	}, nil
}

func (s *minioStore) PresignGet(ctx context.Context, key string) (string, error) {
	ttl := s.cfg.PresignTTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	u, err := s.presignCli.PresignedGetObject(ctx, s.cfg.Bucket, key, ttl, url.Values{})
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// pipeUploader bridges Write calls to a goroutine running PutObject.
type pipeUploader struct {
	pw   *io.PipeWriter
	gz   *gzip.Writer // nil when gzip disabled
	size atomic.Int64
	done chan error
}

func (u *pipeUploader) Write(p []byte) (int, error) {
	w := io.Writer(u.pw)
	if u.gz != nil {
		w = u.gz
	}
	n, err := w.Write(p)
	if n > 0 {
		u.size.Add(int64(n))
	}
	return n, err
}

func (u *pipeUploader) Close() error {
	// Order matters: flush gzip → close pipe (so PutObject sees EOF) → wait for
	// upload to finish so the caller knows whether the object actually landed.
	var firstErr error
	if u.gz != nil {
		if err := u.gz.Close(); err != nil {
			firstErr = err
			_ = u.pw.CloseWithError(err)
		}
	}
	if firstErr == nil {
		if err := u.pw.Close(); err != nil {
			firstErr = err
		}
	}
	uploadErr := <-u.done
	if firstErr != nil {
		return firstErr
	}
	return uploadErr
}

func (u *pipeUploader) BytesWritten() int64 { return u.size.Load() }
