package cli

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/config"
	"github.com/irisvn/kiro-let-go/internal/kiro"
	"github.com/irisvn/kiro-let-go/internal/logging"
	"github.com/irisvn/kiro-let-go/internal/version"
	"github.com/spf13/cobra"
)

var reservedCommandNames = map[string]struct{}{
	"help":             {},
	"completion":       {},
	"__complete":       {},
	"__completeNoDesc": {},
}

type CLI struct {
	configPath string
	dbPath     string
	jsonOutput bool

	cfg     *config.Config
	db      *sql.DB
	store   *account.Store
	manager *account.Manager
	fetcher *account.Fetcher
	circuit *account.CircuitBreaker
	logger  *slog.Logger
}

func NewRootCmd() (*cobra.Command, func()) {
	c := &CLI{}
	root := &cobra.Command{
		Use:               "kiro-let-go-cli",
		Short:             "Kiro Let Go CLI",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return c.init(cmd.Context()) },
		SilenceErrors:     true,
		SilenceUsage:      true,
	}

	root.PersistentFlags().StringVar(&c.configPath, "config", "configs/config.json", "config file path")
	root.PersistentFlags().StringVar(&c.dbPath, "db", "", "override SQLite database path")
	root.PersistentFlags().BoolVar(&c.jsonOutput, "json", false, "output JSON instead of table")

	root.AddCommand(c.newVersionCmd())
	root.AddCommand(c.newAccountCmd())
	root.AddCommand(c.newQuotaCmd())
	root.AddCommand(c.newServerCmd())

	return root, func() { c.close() }
}

func Execute() error {
	root, cleanup := NewRootCmd()
	defer cleanup()
	if err := validateCommandTree(root); err != nil {
		return err
	}
	return root.Execute()
}

func (c *CLI) newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "version",
		Short:             "Print the build version",
		Args:              cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version.Version)
			return err
		},
	}
}

func (c *CLI) init(_ context.Context) error {
	cfg, err := config.Load(c.configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	c.cfg = cfg

	if c.dbPath != "" {
		cfg.Storage.SQLitePath = c.dbPath
	}

	if path := cfg.Storage.SQLitePath; path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("database does not exist: %s", path)
			}
			return fmt.Errorf("stat database: %w", err)
		}
	}

	db, err := account.OpenDB(cfg.Storage.SQLitePath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	c.db = db

	store, err := account.NewStore(db)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("create store: %w", err)
	}
	c.store = store

	c.logger = logging.New(config.LoggingConfig{Level: "error", Format: "text"})

	c.circuit = account.NewCircuitBreaker(account.CircuitConfig{
		BaseCooldown:             time.Duration(cfg.Failover.BaseCooldownSec) * time.Second,
		MaxBackoffMultiplier:     cfg.Failover.MaxBackoffMultiplier,
		ProbabilisticRetryChance: cfg.Failover.ProbabilisticRetryChance,
	}, nil)

	balancer := &account.RoundRobin{}
	socialAuth := kiro.NewSocialAuth(&http.Client{Timeout: 30 * time.Second}, c.logger)
	apikeyAuth := kiro.NewAPIKeyAuth()
	c.manager = account.NewManager(store, balancer, c.circuit, account.ManagerConfig{
		StickySession: cfg.LoadBalancer.StickySession,
		DefaultRegion: cfg.Kiro.Region,
	}, c.logger,
		account.WithSocialAuth(socialAuth),
		account.WithAPIKeyAuth(apikeyAuth),
	)

	ttl := time.Duration(cfg.Quota.CacheTTLSeconds) * time.Second
	c.fetcher = account.NewFetcher(http.DefaultClient, store, ttl, c.logger)

	return nil
}

func (c *CLI) close() {
	if c.store != nil {
		_ = c.store.Close()
	}
	if c.db != nil {
		_ = c.db.Close()
	}
}

func validateCommandTree(cmd *cobra.Command) error {
	if _, reserved := reservedCommandNames[cmd.Name()]; reserved {
		return fmt.Errorf("command name %q conflicts with cobra reserved names", cmd.Name())
	}
	for _, child := range cmd.Commands() {
		if err := validateCommandTree(child); err != nil {
			return err
		}
	}
	return nil
}
