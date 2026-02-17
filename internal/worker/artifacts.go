package worker

import (
	"bytes"
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/vultisig/plugin-tests/config"
)

type ArtifactUploader struct {
	cfg config.S3Config
}

func NewArtifactUploader(cfg config.S3Config) *ArtifactUploader {
	return &ArtifactUploader{cfg: cfg}
}

func (u *ArtifactUploader) UploadRunArtifacts(ctx context.Context, runID string, result *RunResult) (string, error) {
	if u.cfg.Bucket == "" {
		return "", nil
	}

	sess, err := session.NewSession(&aws.Config{
		Region:           aws.String(u.cfg.Region),
		Endpoint:         aws.String(u.cfg.Endpoint),
		Credentials:      credentials.NewStaticCredentials(u.cfg.AccessKey, u.cfg.SecretKey, ""),
		S3ForcePathStyle: aws.Bool(true),
	})
	if err != nil {
		return "", fmt.Errorf("failed to create S3 session: %w", err)
	}

	client := s3.New(sess)
	prefix := runID

	if result.SeederLogs != "" {
		err = u.upload(ctx, client, prefix+"/seeder.txt", result.SeederLogs)
		if err != nil {
			return prefix, fmt.Errorf("failed to upload seeder logs: %w", err)
		}
	}

	if result.TestLogs != "" {
		err = u.upload(ctx, client, prefix+"/test.txt", result.TestLogs)
		if err != nil {
			return prefix, fmt.Errorf("failed to upload test logs: %w", err)
		}
	}

	return prefix, nil
}

func (u *ArtifactUploader) upload(ctx context.Context, client *s3.S3, key, content string) error {
	_, err := client.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(u.cfg.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader([]byte(content)),
		ContentType: aws.String("text/plain"),
	})
	return err
}
