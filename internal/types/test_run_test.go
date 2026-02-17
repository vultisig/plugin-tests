package types

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vultisig/plugin-tests/internal/storage/postgres/queries"
)

func TestTestRunFromQuery(t *testing.T) {
	fixedTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	fixedUUID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")

	t.Run("all fields valid", func(t *testing.T) {
		q := &queries.TestRun{
			ID:             pgtype.UUID{Bytes: fixedUUID, Valid: true},
			PluginID:       "my-plugin",
			ProposalID:     pgtype.Text{String: "prop-123", Valid: true},
			Version:        pgtype.Text{String: "v1.0.0", Valid: true},
			Status:         queries.TestRunStatusPASSED,
			RequestedBy:    "tester",
			ArtifactPrefix: pgtype.Text{String: "/artifacts/123", Valid: true},
			ErrorMessage:   pgtype.Text{String: "some error", Valid: true},
			StartedAt:      pgtype.Timestamptz{Time: fixedTime, Valid: true},
			FinishedAt:     pgtype.Timestamptz{Time: fixedTime.Add(time.Hour), Valid: true},
			CreatedAt:      pgtype.Timestamptz{Time: fixedTime, Valid: true},
			UpdatedAt:      pgtype.Timestamptz{Time: fixedTime.Add(time.Hour), Valid: true},
		}

		result := TestRunFromQuery(q)

		assert.Equal(t, fixedUUID, result.ID)
		assert.Equal(t, "my-plugin", result.PluginID)
		assert.Equal(t, TestRunStatus("PASSED"), result.Status)
		assert.Equal(t, "tester", result.RequestedBy)

		require.NotNil(t, result.ProposalID)
		assert.Equal(t, "prop-123", *result.ProposalID)

		require.NotNil(t, result.Version)
		assert.Equal(t, "v1.0.0", *result.Version)

		require.NotNil(t, result.ArtifactPrefix)
		assert.Equal(t, "/artifacts/123", *result.ArtifactPrefix)

		require.NotNil(t, result.ErrorMessage)
		assert.Equal(t, "some error", *result.ErrorMessage)

		require.NotNil(t, result.StartedAt)
		assert.Equal(t, fixedTime, *result.StartedAt)

		require.NotNil(t, result.FinishedAt)
		assert.Equal(t, fixedTime.Add(time.Hour), *result.FinishedAt)

		assert.Equal(t, fixedTime, result.CreatedAt)
		assert.Equal(t, fixedTime.Add(time.Hour), result.UpdatedAt)
	})

	t.Run("all nullable fields invalid", func(t *testing.T) {
		q := &queries.TestRun{
			ID:          pgtype.UUID{Bytes: fixedUUID, Valid: true},
			PluginID:    "plugin",
			Status:      queries.TestRunStatusQUEUED,
			RequestedBy: "user",
			CreatedAt:   pgtype.Timestamptz{Time: fixedTime, Valid: true},
			UpdatedAt:   pgtype.Timestamptz{Time: fixedTime, Valid: true},
		}

		result := TestRunFromQuery(q)

		assert.Nil(t, result.ProposalID)
		assert.Nil(t, result.Version)
		assert.Nil(t, result.ArtifactPrefix)
		assert.Nil(t, result.ErrorMessage)
		assert.Nil(t, result.StartedAt)
		assert.Nil(t, result.FinishedAt)
	})

	t.Run("partial nulls", func(t *testing.T) {
		q := &queries.TestRun{
			ID:           pgtype.UUID{Bytes: fixedUUID, Valid: true},
			PluginID:     "plugin",
			ProposalID:   pgtype.Text{String: "prop-1", Valid: true},
			Status:       queries.TestRunStatusRUNNING,
			RequestedBy:  "user",
			ErrorMessage: pgtype.Text{String: "err", Valid: true},
			CreatedAt:    pgtype.Timestamptz{Time: fixedTime, Valid: true},
			UpdatedAt:    pgtype.Timestamptz{Time: fixedTime, Valid: true},
		}

		result := TestRunFromQuery(q)

		require.NotNil(t, result.ProposalID)
		assert.Equal(t, "prop-1", *result.ProposalID)

		assert.Nil(t, result.Version)

		require.NotNil(t, result.ErrorMessage)
		assert.Equal(t, "err", *result.ErrorMessage)

		assert.Nil(t, result.ArtifactPrefix)
		assert.Nil(t, result.StartedAt)
		assert.Nil(t, result.FinishedAt)
	})

	t.Run("id invalid", func(t *testing.T) {
		q := &queries.TestRun{
			ID:          pgtype.UUID{Valid: false},
			PluginID:    "plugin",
			Status:      queries.TestRunStatusQUEUED,
			RequestedBy: "user",
			CreatedAt:   pgtype.Timestamptz{Time: fixedTime, Valid: true},
			UpdatedAt:   pgtype.Timestamptz{Time: fixedTime, Valid: true},
		}

		result := TestRunFromQuery(q)
		assert.Equal(t, uuid.Nil, result.ID)
	})

	t.Run("timestamps invalid", func(t *testing.T) {
		q := &queries.TestRun{
			ID:          pgtype.UUID{Bytes: fixedUUID, Valid: true},
			PluginID:    "plugin",
			Status:      queries.TestRunStatusQUEUED,
			RequestedBy: "user",
		}

		result := TestRunFromQuery(q)
		assert.True(t, result.CreatedAt.IsZero())
		assert.True(t, result.UpdatedAt.IsZero())
	})
}
