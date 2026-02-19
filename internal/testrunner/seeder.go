package testrunner

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	vaultType "github.com/vultisig/commondata/go/vultisig/vault/v1"
	recipetypes "github.com/vultisig/recipes/types"
	"github.com/vultisig/vultisig-go/common"
	"google.golang.org/protobuf/proto"

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

		apiKey := fmt.Sprintf("integration-test-apikey-%s", plugin.ID)
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

	vaultPubkey := s.config.Fixture.Vault.PublicKey
	if vaultPubkey != "" {
		tokenID := "integration-token-1"
		now := time.Now()
		expiresAt := now.Add(365 * 24 * time.Hour)

		s.logger.Info("inserting vault token")

		err = queries.UpsertVaultToken(ctx, &db.UpsertVaultTokenParams{
			TokenID:    tokenID,
			PublicKey:  vaultPubkey,
			ExpiresAt:  pgtype.Timestamptz{Time: expiresAt, Valid: true},
			LastUsedAt: pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to insert vault token: %w", err)
		}

		s.logger.Info("inserting test policies")

		targetAddr := "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb0"
		permissiveRecipe, recipeErr := generatePermissivePolicy(targetAddr)
		if recipeErr != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to generate permissive policy: %w", recipeErr)
		}

		for i, plugin := range s.config.Plugins {
			policyID := fmt.Sprintf("00000000-0000-0000-0000-0000000000%02d", i+11)
			parsed, parseErr := uuid.Parse(policyID)
			if parseErr != nil {
				_ = tx.Rollback(ctx)
				return fmt.Errorf("failed to parse policy UUID %s: %w", policyID, parseErr)
			}

			err = queries.UpsertPluginPolicy(ctx, &db.UpsertPluginPolicyParams{
				ID:            pgtype.UUID{Bytes: parsed, Valid: true},
				PublicKey:     vaultPubkey,
				PluginID:      plugin.ID,
				PluginVersion: "1.0.0",
				PolicyVersion: 1,
				Signature:     "0x0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
				Recipe:        permissiveRecipe,
				Active:        true,
			})
			if err != nil {
				_ = tx.Rollback(ctx)
				return fmt.Errorf("failed to insert policy for plugin %s: %w", plugin.ID, err)
			}

			s.logger.WithFields(logrus.Fields{
				"policy_id": policyID,
				"plugin_id": plugin.ID,
			}).Info("policy seeded")
		}
	}

	err = tx.Commit(ctx)
	if err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logger.Info("database seeding completed")
	return nil
}

func (s *Seeder) SeedVaults(ctx context.Context) error {
	vaultData, err := base64.StdEncoding.DecodeString(s.config.Fixture.Vault.VaultB64)
	if err != nil {
		return fmt.Errorf("failed to decode vault_b64: %w", err)
	}

	encryptedVault, err := common.EncryptVault(s.config.EncryptionSecret, vaultData)
	if err != nil {
		return fmt.Errorf("failed to encrypt vault: %w", err)
	}

	vaultContainer := &vaultType.VaultContainer{
		Version:     1,
		Vault:       base64.StdEncoding.EncodeToString(encryptedVault),
		IsEncrypted: true,
	}

	containerBytes, err := proto.Marshal(vaultContainer)
	if err != nil {
		return fmt.Errorf("failed to marshal vault container: %w", err)
	}

	vaultBackup := []byte(base64.StdEncoding.EncodeToString(containerBytes))

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

	s.logger.Info("seeding vault fixtures to MinIO")

	for _, plugin := range s.config.Plugins {
		key := fmt.Sprintf("%s-%s.vult", plugin.ID, s.config.Fixture.Vault.PublicKey)

		_, err = s3Client.PutObject(&s3.PutObjectInput{
			Bucket:      aws.String(s.config.S3.Bucket),
			Key:         aws.String(key),
			Body:        bytes.NewReader(vaultBackup),
			ContentType: aws.String("application/octet-stream"),
		})
		if err != nil {
			return fmt.Errorf("failed to upload %s: %w", key, err)
		}

		s.logger.WithField("key", key).Info("uploaded vault file")
	}

	billingKey := fmt.Sprintf("vultisig-fees-feee-%s.vult", s.config.Fixture.Vault.PublicKey)
	_, err = s3Client.PutObject(&s3.PutObjectInput{
		Bucket:      aws.String(s.config.S3.Bucket),
		Key:         aws.String(billingKey),
		Body:        bytes.NewReader(vaultBackup),
		ContentType: aws.String("application/octet-stream"),
	})
	if err != nil {
		return fmt.Errorf("failed to upload billing vault %s: %w", billingKey, err)
	}

	s.logger.WithField("key", billingKey).Info("uploaded billing vault")
	s.logger.Info("vault seeding completed")
	return nil
}

func generatePermissivePolicy(targetAddr string) (string, error) {
	policy := &recipetypes.Policy{
		Id:          "permissive-test-policy",
		Name:        "Permissive Test Policy",
		Description: "Allows all transactions for testing",
		Version:     1,
		Author:      "integration-test",
		Rules: []*recipetypes.Rule{
			{
				Id:          "allow-ethereum-eth-transfer",
				Resource:    "ethereum.eth.transfer",
				Effect:      recipetypes.Effect_EFFECT_ALLOW,
				Description: "Allow Ethereum transfers",
				Target: &recipetypes.Target{
					TargetType: recipetypes.TargetType_TARGET_TYPE_ADDRESS,
					Target: &recipetypes.Target_Address{
						Address: targetAddr,
					},
				},
				ParameterConstraints: []*recipetypes.ParameterConstraint{
					{
						ParameterName: "amount",
						Constraint: &recipetypes.Constraint{
							Type: recipetypes.ConstraintType_CONSTRAINT_TYPE_ANY,
						},
					},
				},
			},
			{
				Id:          "allow-ethereum-erc20-transfer",
				Resource:    "ethereum.erc20.transfer",
				Effect:      recipetypes.Effect_EFFECT_ALLOW,
				Description: "Allow ERC20 transfers",
				Target: &recipetypes.Target{
					TargetType: recipetypes.TargetType_TARGET_TYPE_ADDRESS,
					Target: &recipetypes.Target_Address{
						Address: targetAddr,
					},
				},
			},
			{
				Id:          "allow-ethereum-erc20-approve",
				Resource:    "ethereum.erc20.approve",
				Effect:      recipetypes.Effect_EFFECT_ALLOW,
				Description: "Allow ERC20 approvals",
				Target: &recipetypes.Target{
					TargetType: recipetypes.TargetType_TARGET_TYPE_ADDRESS,
					Target: &recipetypes.Target_Address{
						Address: targetAddr,
					},
				},
			},
		},
	}

	data, err := proto.Marshal(policy)
	if err != nil {
		return "", fmt.Errorf("failed to marshal policy: %w", err)
	}

	return base64.StdEncoding.EncodeToString(data), nil
}
