package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/sirupsen/logrus"

	"github.com/vultisig/plugin-tests/config"
	"github.com/vultisig/plugin-tests/internal/queue"
	"github.com/vultisig/plugin-tests/internal/storage"
)

type Server struct {
	host       string
	port       int
	db         storage.DatabaseStorage
	producer   *queue.Producer
	logger     *logrus.Logger
	echo       *echo.Echo
	artifactS3 config.S3Config
}

func NewServer(cfg *config.APIConfig, db storage.DatabaseStorage, producer *queue.Producer, logger *logrus.Logger) *Server {
	return &Server{
		host:       cfg.Server.Host,
		port:       cfg.Server.Port,
		db:         db,
		producer:   producer,
		logger:     logger,
		artifactS3: cfg.ArtifactS3,
	}
}

func (s *Server) Start(ctx context.Context) error {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	s.echo = e

	e.Use(middleware.Recover())
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogURI:    true,
		LogStatus: true,
		LogMethod: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			s.logger.WithFields(logrus.Fields{
				"method": v.Method,
				"uri":    v.URI,
				"status": v.Status,
			}).Info("request")
			return nil
		},
	}))

	e.GET("/healthz", func(c echo.Context) error {
		return c.String(http.StatusOK, "OK")
	})

	api := e.Group("/integration-tests")
	api.POST("/run", s.handleCreateTestRun)
	api.GET("/run/:id", s.handleGetTestRun)
	api.GET("/run/:id/artifacts/:name", s.handleGetArtifact)
	api.GET("/runs", s.handleListTestRuns)

	e.GET("/results", s.handleResultsList)
	e.GET("/results/:id", s.handleResultsDetail)

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	s.logger.Infof("API server listening on %s", addr)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		shutdownErr := e.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			s.logger.WithError(shutdownErr).Error("API server shutdown error")
		}
	}()

	err := e.Start(addr)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("API server failed: %w", err)
	}

	return nil
}
