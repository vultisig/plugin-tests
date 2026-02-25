package worker

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"

	"github.com/vultisig/plugin-tests/config"
	"github.com/vultisig/plugin-tests/internal/queue"
	"github.com/vultisig/plugin-tests/internal/storage"
	"github.com/vultisig/plugin-tests/internal/storage/postgres/queries"
)

const maxErrorMessageLen = 4096

type Consumer struct {
	db          storage.DatabaseStorage
	k8s         kubernetes.Interface
	logger      *logrus.Logger
	cfg         config.K8sJobConfig
	artifactCfg config.S3Config
}

func NewConsumer(db storage.DatabaseStorage, k8s kubernetes.Interface, logger *logrus.Logger, cfg config.K8sJobConfig, artifactCfg config.S3Config) *Consumer {
	return &Consumer{
		db:          db,
		k8s:         k8s,
		logger:      logger,
		cfg:         cfg,
		artifactCfg: artifactCfg,
	}
}

func (c *Consumer) Handle(ctx context.Context, t *asynq.Task) error {
	var payload queue.TestRunPayload
	err := json.Unmarshal(t.Payload(), &payload)
	if err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	parsed, err := uuid.Parse(payload.RunID)
	if err != nil {
		return fmt.Errorf("invalid run_id %s: %w", payload.RunID, err)
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	nsName := namespaceNameWithPlugin(payload.RunID, payload.PluginID)
	labels := runLabels(payload.RunID, payload.PluginID, kindIntegration)

	log := c.logger.WithFields(logrus.Fields{
		"run_id":    payload.RunID,
		"plugin_id": payload.PluginID,
		"namespace": nsName,
	})
	log.Info("processing test run")

	err = c.db.Queries().UpdateTestRunStarted(ctx, pgID)
	if err != nil {
		return fmt.Errorf("failed to mark run as started: %w", err)
	}

	createdNS := false
	defer func() {
		if !createdNS {
			return
		}
		// TODO: re-enable after debugging
		// cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		// defer cancel()
		// delErr := deleteNamespace(cleanupCtx, c.k8s, nsName)
		// if delErr != nil {
		// 	log.WithError(delErr).Warn("failed to cleanup namespace")
		// } else {
		// 	log.Info("namespace cleaned up")
		// }
		log.Info("namespace cleanup skipped (debug mode)")
	}()

	err = createNamespace(ctx, c.k8s, nsName, labels)
	if err != nil {
		c.finishWithError(ctx, pgID, err.Error(), log)
		return nil
	}
	createdNS = true
	log.Info("namespace created")

	if c.cfg.ImagePullSecret != "" {
		copyErr := copySecret(ctx, c.k8s, c.cfg.SystemNamespace, nsName, c.cfg.ImagePullSecret)
		if copyErr != nil {
			log.WithError(copyErr).Warn("failed to copy image pull secret")
		}
	}
	if c.cfg.TLSSecretName != "" {
		copyErr := copySecret(ctx, c.k8s, c.cfg.SystemNamespace, nsName, c.cfg.TLSSecretName)
		if copyErr != nil {
			log.WithError(copyErr).Warn("failed to copy TLS secret")
		}
	}

	jobCfg := c.cfg
	if payload.PluginEndpoint != "" {
		jobCfg.PluginEndpoint = payload.PluginEndpoint
	}
	if payload.PluginAPIKey != "" {
		jobCfg.PluginAPIKey = payload.PluginAPIKey
	}

	runner := NewRunner(c.k8s, jobCfg, log)
	result := runner.Run(ctx, nsName, payload.RunID, payload.PluginID, labels)

	uploader := NewArtifactUploader(c.artifactCfg)
	artifactPrefix, uploadErr := uploader.UploadRunArtifacts(ctx, payload.RunID, result)
	if uploadErr != nil {
		log.WithError(uploadErr).Warn("failed to upload artifacts")
	}

	if result.ErrorMsg != "" {
		c.finishWithArtifacts(ctx, pgID, queries.TestRunStatusERROR, result.ErrorMsg, artifactPrefix, log)
		return nil
	}

	if result.Passed {
		c.finishWithArtifacts(ctx, pgID, queries.TestRunStatusPASSED, "", artifactPrefix, log)
		log.Info("test run passed")
	} else {
		c.finishWithArtifacts(ctx, pgID, queries.TestRunStatusFAILED, result.ErrorMsg, artifactPrefix, log)
		log.Warn("test run failed")
	}
	return nil
}

func (c *Consumer) finishWithError(ctx context.Context, id pgtype.UUID, errMsg string, log *logrus.Entry) {
	c.finishWithArtifacts(ctx, id, queries.TestRunStatusERROR, errMsg, "", log)
}

func (c *Consumer) finishWithArtifacts(ctx context.Context, id pgtype.UUID, status queries.TestRunStatus, errMsg, artifactPrefix string, log *logrus.Entry) {
	if len(errMsg) > maxErrorMessageLen {
		errMsg = errMsg[:maxErrorMessageLen]
	}
	params := &queries.UpdateTestRunFinishedParams{
		ID:     id,
		Status: status,
	}
	if errMsg != "" {
		params.ErrorMessage = pgtype.Text{String: errMsg, Valid: true}
	}
	if artifactPrefix != "" {
		params.ArtifactPrefix = pgtype.Text{String: artifactPrefix, Valid: true}
	}
	err := c.db.Queries().UpdateTestRunFinished(ctx, params)
	if err != nil {
		log.WithError(err).Error("failed to update test run status")
	}
}
