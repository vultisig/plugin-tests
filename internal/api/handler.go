package api

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"github.com/vultisig/plugin-tests/internal/queue"
	"github.com/vultisig/plugin-tests/internal/storage/postgres/queries"
	"github.com/vultisig/plugin-tests/internal/types"
)

const defaultSuite = "integration"

type CreateTestRunRequest struct {
	PluginID       string `json:"plugin_id"`
	ProposalID     string `json:"proposal_id,omitempty"`
	Version        string `json:"version,omitempty"`
	RequestedBy    string `json:"requested_by"`
	PluginEndpoint string `json:"plugin_endpoint,omitempty"`
	PluginAPIKey   string `json:"plugin_api_key,omitempty"`
}

func (s *Server) handleCreateTestRun(c echo.Context) error {
	var req CreateTestRunRequest
	err := c.Bind(&req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
	}

	req.PluginID = strings.TrimSpace(req.PluginID)
	req.ProposalID = strings.TrimSpace(req.ProposalID)
	req.Version = strings.TrimSpace(req.Version)
	req.RequestedBy = strings.TrimSpace(req.RequestedBy)

	if req.PluginID == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "plugin_id is required"})
	}
	if req.RequestedBy == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "requested_by is required"})
	}

	params := &queries.CreateTestRunParams{
		PluginID:    req.PluginID,
		Status:      queries.TestRunStatusQUEUED,
		RequestedBy: req.RequestedBy,
	}
	if req.ProposalID != "" {
		params.ProposalID = pgtype.Text{String: req.ProposalID, Valid: true}
	}
	if req.Version != "" {
		params.Version = pgtype.Text{String: req.Version, Valid: true}
	}

	ctx := c.Request().Context()

	run, err := s.db.Queries().CreateTestRun(ctx, params)
	if err != nil {
		s.logger.WithError(err).Error("failed to create test run")
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create test run"})
	}

	result := types.TestRunFromQuery(run)

	payload := queue.TestRunPayload{
		RunID:          result.ID.String(),
		PluginID:       req.PluginID,
		ProposalID:     req.ProposalID,
		Version:        req.Version,
		Suite:          defaultSuite,
		RequestedBy:    req.RequestedBy,
		PluginEndpoint: strings.TrimSpace(req.PluginEndpoint),
		PluginAPIKey:   strings.TrimSpace(req.PluginAPIKey),
	}

	_, err = s.producer.EnqueueTestRun(payload)
	if err != nil {
		s.logger.WithError(err).Error("failed to enqueue test run, marking as ERROR")
		errMsg := pgtype.Text{String: "failed to enqueue task", Valid: true}
		updateErr := s.db.Queries().UpdateTestRunFinished(ctx, &queries.UpdateTestRunFinishedParams{
			ID:           run.ID,
			Status:       queries.TestRunStatusERROR,
			ErrorMessage: errMsg,
		})
		if updateErr != nil {
			s.logger.WithError(updateErr).Error("failed to mark test run as error after enqueue failure")
		}
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to enqueue test run"})
	}

	return c.JSON(http.StatusCreated, SuccessResponse{Data: result})
}

func (s *Server) handleGetTestRun(c echo.Context) error {
	idStr := c.Param("id")

	parsed, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid run id"})
	}

	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	run, err := s.db.Queries().GetTestRun(c.Request().Context(), pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c.JSON(http.StatusNotFound, ErrorResponse{Error: "test run not found"})
		}
		s.logger.WithError(err).Error("failed to get test run")
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
	}

	result := types.TestRunFromQuery(run)
	return c.JSON(http.StatusOK, SuccessResponse{Data: result})
}

func (s *Server) handleListTestRuns(c echo.Context) error {
	limit := 20
	offset := 0

	if v := c.QueryParam("limit"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 1 || parsed > 100 {
			return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid limit parameter"})
		}
		limit = parsed
	}
	if v := c.QueryParam("offset"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 0 || parsed > math.MaxInt32 {
			return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid offset parameter"})
		}
		offset = parsed
	}

	filterParams, err := buildFilterParams(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}

	ctx := c.Request().Context()

	runs, err := s.db.Queries().ListTestRuns(ctx, &queries.ListTestRunsParams{
		PluginID:    filterParams.PluginID,
		Status:      filterParams.Status,
		QueryLimit:  int32(limit),
		QueryOffset: int32(offset),
	})
	if err != nil {
		s.logger.WithError(err).Error("failed to list test runs")
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to list test runs"})
	}

	total, err := s.db.Queries().CountTestRuns(ctx, &queries.CountTestRunsParams{
		PluginID: filterParams.PluginID,
		Status:   filterParams.Status,
	})
	if err != nil {
		s.logger.WithError(err).Error("failed to count test runs")
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to count test runs"})
	}

	items := make([]types.TestRun, 0, len(runs))
	for _, r := range runs {
		items = append(items, types.TestRunFromQuery(r))
	}

	return c.JSON(http.StatusOK, ListResponse{
		Data:   items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

type filterParams struct {
	PluginID pgtype.Text
	Status   queries.NullTestRunStatus
}

var validStatuses = map[string]bool{
	"QUEUED":  true,
	"RUNNING": true,
	"PASSED":  true,
	"FAILED":  true,
	"ERROR":   true,
}

func buildFilterParams(c echo.Context) (filterParams, error) {
	var fp filterParams
	if v := strings.TrimSpace(c.QueryParam("plugin_id")); v != "" {
		fp.PluginID = pgtype.Text{String: v, Valid: true}
	}
	if v := strings.TrimSpace(c.QueryParam("status")); v != "" {
		if !validStatuses[v] {
			return fp, fmt.Errorf("invalid status: %s", v)
		}
		fp.Status = queries.NullTestRunStatus{
			TestRunStatus: queries.TestRunStatus(v),
			Valid:         true,
		}
	}
	return fp, nil
}
