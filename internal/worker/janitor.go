package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/vultisig/plugin-tests/config"
	"github.com/vultisig/plugin-tests/internal/storage"
	"github.com/vultisig/plugin-tests/internal/storage/postgres/queries"
)

type Janitor struct {
	db     storage.DatabaseStorage
	k8s    kubernetes.Interface
	logger *logrus.Logger
	cfg    config.JanitorConfig
}

func NewJanitor(db storage.DatabaseStorage, k8s kubernetes.Interface, logger *logrus.Logger, cfg config.JanitorConfig) *Janitor {
	return &Janitor{
		db:     db,
		k8s:    k8s,
		logger: logger,
		cfg:    cfg,
	}
}

func (j *Janitor) Run(ctx context.Context) {
	if j.cfg.Interval <= 0 || j.cfg.StaleThreshold <= 0 {
		j.logger.Warn("janitor disabled (interval or stale_threshold <= 0)")
		return
	}

	ticker := time.NewTicker(j.cfg.Interval)
	defer ticker.Stop()

	j.logger.WithFields(logrus.Fields{
		"interval":        j.cfg.Interval,
		"stale_threshold": j.cfg.StaleThreshold,
	}).Info("janitor started")

	for {
		select {
		case <-ctx.Done():
			j.logger.Info("janitor stopped")
			return
		case <-ticker.C:
			j.sweep(ctx)
		}
	}
}

func (j *Janitor) sweep(ctx context.Context) {
	j.cleanStaleNamespaces(ctx)
	j.markStaleRuns(ctx)
}

func (j *Janitor) cleanStaleNamespaces(ctx context.Context) {
	nsList, err := j.k8s.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", labelManagedBy, managedByValue),
	})
	if err != nil {
		j.logger.WithError(err).Error("janitor: failed to list namespaces")
		return
	}

	now := time.Now()
	threshold := now.Add(-j.cfg.StaleThreshold)
	for _, ns := range nsList.Items {
		if ns.CreationTimestamp.Time.Before(threshold) {
			j.logger.WithField("namespace", ns.Name).Info("janitor: deleting stale namespace")
			delCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err = deleteNamespace(delCtx, j.k8s, ns.Name)
			cancel()
			if err != nil {
				j.logger.WithError(err).WithField("namespace", ns.Name).Warn("janitor: failed to delete namespace")
			}
		}
	}
}

func (j *Janitor) markStaleRuns(ctx context.Context) {
	interval := pgtype.Interval{
		Microseconds: j.cfg.StaleThreshold.Microseconds(),
		Valid:        true,
	}
	staleRuns, err := j.db.Queries().GetStaleRunningRuns(ctx, interval)
	if err != nil {
		j.logger.WithError(err).Error("janitor: failed to get stale runs")
		return
	}

	for _, run := range staleRuns {
		j.logger.WithField("run_id", run.ID).Info("janitor: marking stale run as ERROR")
		err = j.db.Queries().MarkStaleRunAsError(ctx, &queries.MarkStaleRunAsErrorParams{
			ID:           run.ID,
			ErrorMessage: pgtype.Text{String: "marked as stale by janitor", Valid: true},
		})
		if err != nil {
			j.logger.WithError(err).WithField("run_id", run.ID).Warn("janitor: failed to mark run as error")
		}
	}
}
