package api

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"github.com/vultisig/plugin-tests/config"
)

var allowedArtifacts = map[string]bool{
	"seeder.txt": true,
	"test.txt":   true,
}

func (s *Server) handleGetArtifact(c echo.Context) error {
	idStr := c.Param("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid run id"})
	}

	name := c.Param("name")
	if !allowedArtifacts[name] {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid artifact name"})
	}

	pgID := pgtype.UUID{Bytes: parsed, Valid: true}
	run, err := s.db.Queries().GetTestRun(c.Request().Context(), pgID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.JSON(http.StatusNotFound, ErrorResponse{Error: "test run not found"})
		}
		s.logger.WithError(err).Error("failed to get test run")
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
	}

	if !run.ArtifactPrefix.Valid || run.ArtifactPrefix.String == "" {
		return c.JSON(http.StatusNotFound, ErrorResponse{Error: "no artifacts for this run"})
	}

	key := run.ArtifactPrefix.String + "/" + name
	content, err := readArtifact(c.Request().Context(), s.artifactS3, key)
	if err != nil {
		s.logger.WithError(err).WithField("key", key).Error("failed to read artifact from S3")
		return c.JSON(http.StatusNotFound, ErrorResponse{Error: "artifact not found"})
	}

	return c.String(http.StatusOK, content)
}

func readArtifact(ctx context.Context, cfg config.S3Config, key string) (string, error) {
	sess, err := session.NewSession(&aws.Config{
		Region:           aws.String(cfg.Region),
		Endpoint:         aws.String(cfg.Endpoint),
		Credentials:      credentials.NewStaticCredentials(cfg.AccessKey, cfg.SecretKey, ""),
		S3ForcePathStyle: aws.Bool(true),
	})
	if err != nil {
		return "", fmt.Errorf("failed to create S3 session: %w", err)
	}

	client := s3.New(sess)
	out, err := client.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", fmt.Errorf("failed to get S3 object: %w", err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read S3 object body: %w", err)
	}

	return string(data), nil
}
