package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/config"
	"github.com/irisvn/kiro-let-go/internal/kiro"
	"github.com/irisvn/kiro-let-go/internal/logging"
	"github.com/irisvn/kiro-let-go/internal/server"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

const (
	exitOK             = 0
	exitStartupFailure = 1
	exitUnclean        = 2
	shutdownTimeout    = 30 * time.Second
)

var errUncleanShutdown = errors.New("unclean shutdown")

type application struct {
	logger         *slog.Logger
	server         *server.Server
	watcher        *account.Watcher
	tokenRefresher *account.TokenRefresher
	store          *account.Store
	db             *sql.DB
}

func main() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	os.Exit(runMain(context.Background(), os.Args[1:], sigCh, os.Stderr))
}

func runMain(ctx context.Context, args []string, sigCh <-chan os.Signal, stderr io.Writer) int {
	app, helpShown, err := buildApplication(ctx, args, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return exitStartupFailure
	}
	if helpShown {
		return exitOK
	}
	defer func() {
		if closeErr := app.close(); closeErr != nil {
			app.logger.Warn("resource cleanup failed", "error", closeErr)
		}
	}()

	if err := runApplication(ctx, app, sigCh); err != nil {
		if errors.Is(err, errUncleanShutdown) {
			_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
			app.logger.Error("server shutdown did not complete cleanly", "error", err)
			return exitUnclean
		}
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		app.logger.Error("server exited with error", "error", err)
		return exitStartupFailure
	}

	return exitOK

}

func buildApplication(ctx context.Context, args []string, stderr io.Writer) (*application, bool, error) {
	flagSet := newFlagSet(stderr)
	parsedArgs := normalizeArgs(args)
	if err := flagSet.Parse(parsedArgs); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("parse flags: %w", err)
	}
	if flagSet.NArg() != 0 {
		return nil, false, fmt.Errorf("unexpected arguments: %s", strings.Join(flagSet.Args(), " "))
	}

	configPath, _ := flagSet.GetString("config")
	cfg, err := config.LoadWithFlags(configPath, flagSet)
	if err != nil {
		return nil, false, fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, false, fmt.Errorf("validate config: %w", err)
	}

	logger := logging.New(cfg.Logging)
	if err := ensureSQLiteParent(cfg.Storage.SQLitePath); err != nil {
		return nil, false, err
	}

	db, err := account.OpenDB(cfg.Storage.SQLitePath)
	if err != nil {
		return nil, false, fmt.Errorf("open sqlite: %w", err)
	}

	if err := account.Apply(ctx, db); err != nil {
		_ = db.Close()
		return nil, false, fmt.Errorf("apply migrations: %w", err)
	}

	dynamicCfg := config.NewDynamicConfig(db)
	if err := dynamicCfg.Load(); err != nil {
		_ = db.Close()
		return nil, false, fmt.Errorf("load dynamic config: %w", err)
	}
	if dynamicCfg.IsEmpty() {
		if err := dynamicCfg.SeedFromStatic(cfg); err != nil {
			_ = db.Close()
			return nil, false, fmt.Errorf("seed dynamic config: %w", err)
		}
	}

	store, err := account.NewStore(db)
	if err != nil {
		_ = db.Close()
		return nil, false, fmt.Errorf("create account store: %w", err)
	}

	circuit := account.NewCircuitBreaker(account.CircuitConfig{
		BaseCooldown:             time.Duration(cfg.Failover.BaseCooldownSec) * time.Second,
		MaxBackoffMultiplier:     cfg.Failover.MaxBackoffMultiplier,
		ProbabilisticRetryChance: cfg.Failover.ProbabilisticRetryChance,
	}, nil)
	circuit.SetDynamicConfig(dynamicCfg)
	if err := seedCircuitBreaker(ctx, store, circuit); err != nil {
		_ = store.Close()
		_ = db.Close()
		return nil, false, err
	}

	socialAuth := kiro.NewSocialAuth(&http.Client{Timeout: shutdownTimeout}, logger)
	apikeyAuth := kiro.NewAPIKeyAuth()
	client := kiro.NewClient(0, logger)
	quotaFetcher := account.NewFetcher(&http.Client{Timeout: 30 * time.Second}, store, time.Duration(cfg.Quota.CacheTTLSeconds)*time.Second, logger)
	quotaFetcher.SetDynamicConfig(dynamicCfg)
	balancer := account.NewDynamicBalancer(dynamicCfg, quotaFetcher)

	manager := account.NewManager(
		store,
		balancer,
		circuit,
		account.ManagerConfig{
			StickySession: cfg.LoadBalancer.StickySession,
			DefaultRegion: firstNonEmpty(cfg.Kiro.APIRegion, cfg.Kiro.Region, cfg.Kiro.AuthRegion, "us-east-1"),
		},
		logger,
		account.WithSocialAuth(socialAuth),
		account.WithAPIKeyAuth(apikeyAuth),
		account.WithDynamicConfig(dynamicCfg),
	)
	modelMapper := kiro.NewModelMapper(dynamicCfg.Get().ModelMappings)
	dispatcher := kiro.NewDispatcher(client, manager, kiro.DispatcherConfig{MaxAttempts: cfg.Failover.MaxAttempts, ModelMapper: modelMapper, DynamicConfig: dynamicCfg}, logger)
	srv := server.New(server.Deps{
		Cfg:          cfg,
		Logger:       logger,
		Store:        store,
		Manager:      manager,
		Dispatcher:   dispatcher,
		QuotaFetcher: quotaFetcher,
		Circuit:      circuit,
		DynamicCfg:   dynamicCfg,
	})

	var watcher *account.Watcher
	if strings.TrimSpace(cfg.Storage.CredentialsJSONPath) != "" {
		watcher = account.NewWatcher(cfg.Storage.CredentialsJSONPath, store, logger)
	}

	tokenRefresher := account.NewTokenRefresher(store, socialAuth, logger)
	return &application{logger: logger, server: srv, watcher: watcher, tokenRefresher: tokenRefresher, store: store, db: db}, false, nil
}

func runApplication(ctx context.Context, app *application, sigCh <-chan os.Signal) error {
	if app == nil || app.server == nil {
		return fmt.Errorf("application is not initialized")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	group, groupCtx := errgroup.WithContext(runCtx)
	var signalSeen atomic.Bool

	group.Go(func() error {
		if err := app.server.Run(groupCtx); err != nil {
			return fmt.Errorf("run http server: %w", err)
		}
		return nil
	})

	if app.watcher != nil {
		group.Go(func() error {
			err := app.watcher.Run(groupCtx)
			if err == nil || errors.Is(err, context.Canceled) {
				return nil
			}
			app.logger.Warn("credentials watcher exited with error", "error", err)
			return fmt.Errorf("run credentials watcher: %w", err)
		})
	}

	if app.tokenRefresher != nil {
		group.Go(func() error {
			return app.tokenRefresher.Run(groupCtx)
		})
	}

	group.Go(func() error {
		select {
		case <-groupCtx.Done():
			return nil
		case sig := <-sigCh:
			if sig == nil {
				cancel()
				return nil
			}
			signalSeen.Store(true)
			app.logger.Info("shutdown signal received", "signal", sig.String())
			cancel()
			return nil
		}
	})

	err := group.Wait()
	if err != nil && signalSeen.Load() && errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %v", errUncleanShutdown, err)
	}
	return err
}

func (a *application) close() error {
	if a == nil {
		return nil
	}

	var firstErr error
	if a.store != nil {
		if err := a.store.Close(); err != nil {
			firstErr = fmt.Errorf("close account store: %w", err)
		}
	}
	if a.db != nil {
		if err := a.db.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close sqlite db: %w", err)
		}
	}
	return firstErr
}

func newFlagSet(stderr io.Writer) *pflag.FlagSet {
	flags := pflag.NewFlagSet("server", pflag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		return pflag.NormalizedName(strings.ReplaceAll(name, "-", "_"))
	})

	flags.String("config", "", "Path to a JSON config file")
	flags.String("server.host", "", "Server bind host")
	flags.Int("server.port", 0, "Server bind port")
	flags.String("server.admin_api_key", "", "Admin API key")
	flags.String("server.proxy_api_key", "", "Proxy API key")
	flags.String("kiro.region", "", "Default Kiro region")
	flags.String("kiro.auth_region", "", "Default Kiro auth region")
	flags.String("kiro.api_region", "", "Default Kiro API region")
	flags.String("storage.sqlite_path", "", "SQLite database path")
	flags.String("storage.credentials_json_path", "", "Credentials JSON file path")
	flags.String("load_balancer.strategy", "", "Load-balancer strategy")
	flags.Bool("load_balancer.sticky_session", false, "Enable sticky sessions")
	flags.Int("quota.cache_ttl_seconds", 0, "Quota cache TTL in seconds")
	flags.Int("failover.base_cooldown_sec", 0, "Circuit-breaker base cooldown in seconds")
	flags.Int("failover.max_backoff_multiplier", 0, "Circuit-breaker max backoff multiplier")
	flags.Float64("failover.probabilistic_retry_chance", 0, "Circuit-breaker retry chance")
	flags.Int("failover.max_attempts", 0, "Dispatcher max attempts")
	flags.String("logging.level", "", "Log level")
	flags.String("logging.format", "", "Log format")

	return flags
}

func normalizeArgs(args []string) []string {
	if len(args) > 0 && args[0] == "server" {
		return args[1:]
	}
	return args
}

func ensureSQLiteParent(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create sqlite directory %q: %w", dir, err)
	}
	return nil
}

func seedCircuitBreaker(ctx context.Context, store *account.Store, circuit *account.CircuitBreaker) error {
	accounts, err := store.List(ctx, account.ListFilter{})
	if err != nil {
		return fmt.Errorf("load accounts for circuit breaker: %w", err)
	}
	for i := range accounts {
		if accounts[i].FailureCount > 0 {
			circuit.Seed(accounts[i].ID, accounts[i].FailureCount)
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
