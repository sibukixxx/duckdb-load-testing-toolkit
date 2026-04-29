package storage

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Config holds S3-compatible storage configuration
type S3Config struct {
	Endpoint  string // S3-compatible endpoint (e.g., http://rustfs:9000)
	Bucket    string // Bucket name
	AccessKey string // Access key
	SecretKey string // Secret key
	Region    string // Region (default: us-east-1)
}

// S3Uploader handles uploads to S3-compatible storage
type S3Uploader struct {
	client *s3.Client
	config S3Config
}

// NewS3Uploader creates a new S3 uploader
func NewS3Uploader(cfg S3Config) (*S3Uploader, error) {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	ctx := context.Background()
	var awsCfg aws.Config
	var err error

	if cfg.Endpoint != "" {
		// S3-compatible storage (RustFS, MinIO, etc.)
		awsCfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(cfg.Region),
			config.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
			),
		)
	} else {
		// AWS S3
		awsCfg, err = config.LoadDefaultConfig(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	var client *s3.Client
	if cfg.Endpoint != "" {
		client = s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		})
	} else {
		client = s3.NewFromConfig(awsCfg)
	}

	return &S3Uploader{
		client: client,
		config: cfg,
	}, nil
}

// NewS3UploaderFromEnv creates an S3 uploader from environment variables
func NewS3UploaderFromEnv() (*S3Uploader, error) {
	cfg := S3Config{
		Endpoint:  os.Getenv("S3_ENDPOINT"),
		Bucket:    os.Getenv("S3_BUCKET"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
		Region:    os.Getenv("S3_REGION"),
	}

	if cfg.Bucket == "" {
		return nil, nil // No S3 configured
	}

	return NewS3Uploader(cfg)
}

// UploadFile uploads a file to S3
func (u *S3Uploader) UploadFile(ctx context.Context, key, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	uploader := manager.NewUploader(u.client)
	_, err = uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: &u.config.Bucket,
		Key:    &key,
		Body:   f,
	})
	if err != nil {
		return fmt.Errorf("failed to upload: %w", err)
	}

	return nil
}

// DownloadFile downloads a file from S3
func (u *S3Uploader) DownloadFile(ctx context.Context, key, filePath string) error {
	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	downloader := manager.NewDownloader(u.client)
	_, err = downloader.Download(ctx, f, &s3.GetObjectInput{
		Bucket: &u.config.Bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}

	return nil
}

// ListObjects lists objects with a given prefix
func (u *S3Uploader) ListObjects(ctx context.Context, prefix string) ([]string, error) {
	result, err := u.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: &u.config.Bucket,
		Prefix: &prefix,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	var keys []string
	for _, obj := range result.Contents {
		keys = append(keys, *obj.Key)
	}
	return keys, nil
}

// DeleteObject deletes an object from S3
func (u *S3Uploader) DeleteObject(ctx context.Context, key string) error {
	_, err := u.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &u.config.Bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}
	return nil
}

// GetBucket returns the configured bucket name
func (u *S3Uploader) GetBucket() string {
	return u.config.Bucket
}

// IsConfigured returns true if S3 is configured.
// Safe to call on a nil receiver (returns false).
func (u *S3Uploader) IsConfigured() bool {
	if u == nil {
		return false
	}
	return u.config.Bucket != ""
}
