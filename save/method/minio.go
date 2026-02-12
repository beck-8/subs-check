package method

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"mime"
	"path/filepath"

	"github.com/beck-8/subs-check/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ValiS3Config checks if the MinIO configuration is complete.
func ValiS3Config() error {
	if config.GlobalConfig.S3Endpoint == "" {
		return fmt.Errorf("S3Endpoint is not configured")
	}
	if config.GlobalConfig.S3AccessID == "" {
		return fmt.Errorf("S3AccessID is not configured")
	}
	if config.GlobalConfig.S3SecretKey == "" {
		return fmt.Errorf("S3SecretKey is not configured")
	}
	if config.GlobalConfig.S3Bucket == "" {
		return fmt.Errorf("S3Bucket is not configured")
	}
	return nil
}

// UploadToS3 uploads data to a MinIO bucket.
// The 'filename' parameter will be used as the object name in the bucket.
func UploadToS3(data []byte, filename string) error {
	ctx := context.Background()
	endpoint := config.GlobalConfig.S3Endpoint
	accessKeyID := config.GlobalConfig.S3AccessID
	secretAccessKey := config.GlobalConfig.S3SecretKey
	useSSL := config.GlobalConfig.S3UseSSL // e.g., true for HTTPS, false for HTTP
	bucketName := config.GlobalConfig.S3Bucket

	// Initialize minio client object.
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
		BucketLookup: func() minio.BucketLookupType {
			switch config.GlobalConfig.S3BucketLookup {
			case "dns":
				return minio.BucketLookupDNS
			case "path":
				return minio.BucketLookupPath
			case "auto":
				return minio.BucketLookupAuto
			default:
				return minio.BucketLookupAuto
			}
		}(),
	})
	if err != nil {
		return fmt.Errorf("failed to initialize MinIO client: %w", err)
	}

	// Check if the bucket exists.
	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		return fmt.Errorf("failed to check if bucket '%s' exists: %w", bucketName, err)
	}
	if !exists {
		return fmt.Errorf("bucket '%s' does not exist", bucketName)
	}

	// Upload the data.
	reader := bytes.NewReader(data)
	objectName := filename

	ext := filepath.Ext(filename)
	contentType := mime.TypeByExtension(ext)

	if contentType == "" {
		contentType = "application/octet-stream"
		if ext == ".yaml" || ext == ".yml" {
			contentType = "text/plain; charset=utf-8"
		}
	}

	info, err := minioClient.PutObject(ctx, bucketName, objectName, reader, int64(len(data)), minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("failed to upload '%s' to bucket '%s': %w", objectName, bucketName, err)
	}

	slog.Info(fmt.Sprintf("Successfully uploaded '%s' of size %d to bucket '%s'. ETag: %s", objectName, info.Size, bucketName, info.ETag))
	return nil
}
