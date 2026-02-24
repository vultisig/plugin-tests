package testrunner

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sirupsen/logrus"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/plugin-tests/internal/testrunner/db"
)

type S3Config struct {
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	Bucket    string
}

type SeederConfig struct {
	DSN              string
	S3               S3Config
	Fixture          *FixtureData
	Plugins          []PluginConfig
	EncryptionSecret string
}

type Seeder struct {
	config SeederConfig
	logger *logrus.Logger
}

func NewSeeder(cfg SeederConfig, logger *logrus.Logger) *Seeder {
	return &Seeder{config: cfg, logger: logger}
}

func (s *Seeder) SeedDatabase(ctx context.Context) error {
	pool, err := pgxpool.New(ctx, s.config.DSN)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback(ctx)
			panic(r)
		}
	}()

	queries := db.New(pool).WithTx(tx)

	s.logger.Info("seeding integration database")

	for _, plugin := range s.config.Plugins {
		s.logger.WithField("plugin_id", plugin.ID).Info("inserting plugin")

		err = queries.UpsertPlugin(ctx, &db.UpsertPluginParams{
			ID:             plugin.ID,
			Title:          plugin.Title,
			Description:    plugin.Description,
			ServerEndpoint: plugin.ServerEndpoint,
			Category:       db.PluginCategory(plugin.Category),
			Audited:        plugin.Audited,
		})
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to insert plugin %s: %w", plugin.ID, err)
		}

		apiKey := plugin.APIKey
		err = queries.UpsertPluginAPIKey(ctx, &db.UpsertPluginAPIKeyParams{
			PluginID: plugin.ID,
			Apikey:   apiKey,
		})
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to insert API key for plugin %s: %w", plugin.ID, err)
		}

		s.logger.WithField("plugin_id", plugin.ID).Info("plugin seeded")
	}

	now := time.Now()
	tokenExpiry := now.Add(24 * time.Hour)
	err = queries.UpsertVaultToken(ctx, &db.UpsertVaultTokenParams{
		TokenID:    "integration-token-1",
		PublicKey:  s.config.Fixture.Vault.PublicKey,
		ExpiresAt:  pgtype.Timestamptz{Time: tokenExpiry, Valid: true},
		LastUsedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("failed to insert vault token: %w", err)
	}
	s.logger.Info("vault token seeded")

	err = tx.Commit(ctx)
	if err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logger.Info("database seeding completed")
	return nil
}

func (s *Seeder) SeedVaults(ctx context.Context) error {
	sess, err := session.NewSession(&aws.Config{
		Endpoint:         aws.String(s.config.S3.Endpoint),
		Region:           aws.String(s.config.S3.Region),
		Credentials:      credentials.NewStaticCredentials(s.config.S3.AccessKey, s.config.S3.SecretKey, ""),
		S3ForcePathStyle: aws.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("failed to create S3 session: %w", err)
	}

	s3Client := s3.New(sess)

	_, err = s3Client.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(s.config.S3.Bucket),
	})
	if err != nil {
		s.logger.WithError(err).Debug("bucket creation (may already exist)")
	}

	_, err = s3Client.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String("vultisig-dca"),
	})
	if err != nil {
		s.logger.WithError(err).Debug("vultisig-dca bucket creation (may already exist)")
	}

	s.logger.Info("vault seeding completed (buckets created, no vault files uploaded for new-party reshare)")
	return nil
}
