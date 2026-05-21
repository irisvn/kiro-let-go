package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/antiban"
	"github.com/irisvn/kiro-let-go/internal/api/admin"
	"github.com/irisvn/kiro-let-go/internal/api/adminui"
	"github.com/irisvn/kiro-let-go/internal/api/anthropic"
	"github.com/irisvn/kiro-let-go/internal/api/openai"
	"github.com/irisvn/kiro-let-go/internal/config"
	"github.com/irisvn/kiro-let-go/internal/kiro"
	"github.com/irisvn/kiro-let-go/internal/server/middleware"
	"github.com/irisvn/kiro-let-go/internal/version"
)

type Deps struct {
	Cfg          *config.Config
	Logger       *slog.Logger
	Store        *account.Store
	Manager      *account.Manager
	Dispatcher   *kiro.Dispatcher
	QuotaFetcher *account.Fetcher
	Circuit      *account.CircuitBreaker
	RequestLog   *RequestLog
	DynamicCfg   *config.DynamicConfig
}

type Server struct {
	engine       *gin.Engine
	cfg          *config.Config
	logger       *slog.Logger
	manager      *account.Manager
	dispatcher   *kiro.Dispatcher
	quotaFetcher *account.Fetcher
	requestLog   *RequestLog
	boundAddr    chan string
}

func New(deps Deps) *Server {
	r := gin.New()
	r.RedirectTrailingSlash = false

	requestLog := deps.RequestLog
	if requestLog == nil {
		requestLog = NewRequestLogWithFile(100, deps.Cfg.Logging.RequestLogFile)
		_ = requestLog.LoadFromFile()
	}

	r.Use(
		antiban.HealthProbeMiddleware(),
		middleware.RequestIDMiddleware(),
		middleware.LoggingMiddleware(deps.Logger),
		middleware.RecoverMiddleware(deps.Logger),
		middleware.CORSMiddleware(),
		RequestLogMiddleware(requestLog),
	)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "version": version.Version})
	})

	proxyAuth := middleware.ProxyAuthMiddleware(deps.Cfg.Server.ProxyAPIKey)
	anthropicHandler := anthropic.NewHandler(deps.Dispatcher, nil, deps.Logger)
	anthropicHandler.Register(r.Group("", proxyAuth))

	v1 := r.Group("/v1")
	v1.Use(proxyAuth)
	{
		v1.POST("/chat/completions", openai.Handler(openai.HandlerOptions{Dispatcher: deps.Dispatcher}))
		v1.GET("/models", openai.Models)
	}

	adminHandler := admin.NewHandler(
		deps.Store,
		deps.Manager,
		deps.QuotaFetcher,
		deps.Circuit,
		time.Duration(dynamicQuotaTTL(deps))*time.Second,
		deps.Dispatcher,
	)
	adminHandler.SetDynamicConfig(deps.DynamicCfg)
	adminHandler.SetProxyDependencies(deps.Cfg, requestLog, deps.Manager)
	admin.RegisterRoutes(r, deps.Cfg.Server.AdminAPIKey, adminHandler)

	adminui.RegisterRoutes(r)

	return &Server{
		engine:       r,
		cfg:          deps.Cfg,
		logger:       deps.Logger,
		manager:      deps.Manager,
		dispatcher:   deps.Dispatcher,
		quotaFetcher: deps.QuotaFetcher,
		requestLog:   requestLog,
		boundAddr:    make(chan string, 1),
	}
}

func dynamicQuotaTTL(deps Deps) int {
	if deps.DynamicCfg != nil {
		if ttl := deps.DynamicCfg.Get().CacheTTLSeconds; ttl > 0 {
			return ttl
		}
	}
	return deps.Cfg.Quota.CacheTTLSeconds
}

func (s *Server) Run(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Server.Host, s.cfg.Server.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.engine,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			errCh <- err
			return
		}
		actualAddr := ln.Addr().String()
		select {
		case s.boundAddr <- actualAddr:
		default:
		}
		s.logger.Info("server starting", slog.String("addr", actualAddr))
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.logger.Info("shutting down server")
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
