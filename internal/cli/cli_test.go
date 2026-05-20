package cli

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestCLI(t *testing.T) (*CLI, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := account.OpenDB(dbPath)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, account.Apply(ctx, db))

	store, err := account.NewStore(db)
	require.NoError(t, err)

	cfg := &config.Config{
		Server:       config.ServerConfig{Host: "0.0.0.0", Port: 8765},
		Kiro:         config.KiroConfig{Region: "us-east-1", AuthRegion: "us-east-1", APIRegion: "us-east-1"},
		Storage:      config.StorageConfig{SQLitePath: dbPath},
		LoadBalancer: config.LoadBalancerConfig{Strategy: "round_robin", StickySession: true},
		Quota:        config.QuotaConfig{CacheTTLSeconds: 43200},
		Failover:     config.FailoverConfig{BaseCooldownSec: 60, MaxBackoffMultiplier: 1440, ProbabilisticRetryChance: 0.10, MaxAttempts: 9},
		Logging:      config.LoggingConfig{Level: "error", Format: "text"},
	}

	circuit := account.NewCircuitBreaker(account.CircuitConfig{
		BaseCooldown:             time.Duration(cfg.Failover.BaseCooldownSec) * time.Second,
		MaxBackoffMultiplier:     cfg.Failover.MaxBackoffMultiplier,
		ProbabilisticRetryChance: cfg.Failover.ProbabilisticRetryChance,
	}, nil)

	cli := &CLI{
		cfg:     cfg,
		db:      db,
		store:   store,
		circuit: circuit,
		logger:  slog.Default(),
	}

	cleanup := func() {
		_ = store.Close()
		_ = db.Close()
	}

	return cli, cleanup
}

func TestAccountAdd_APIKey(t *testing.T) {
	c, cleanup := setupTestCLI(t)
	defer cleanup()

	err := c.runAccountAdd(context.Background(), "apikey", "test-label", "", "ksk_test123", "", "us-east-1", "", "", "", "", "")
	require.NoError(t, err)

	accs, err := c.store.List(context.Background(), account.ListFilter{})
	require.NoError(t, err)
	require.Len(t, accs, 1)
	assert.Equal(t, "test-label", accs[0].Label)
	assert.Equal(t, "apikey", accs[0].AuthMethod)
	assert.True(t, accs[0].Enabled)
	assert.NotEmpty(t, accs[0].MachineID)
}

func TestAccountAdd_Social(t *testing.T) {
	c, cleanup := setupTestCLI(t)
	defer cleanup()

	err := c.runAccountAdd(context.Background(), "social", "social-label", "rt_test", "", "", "us-west-2", "", "", "", "", "")
	require.NoError(t, err)

	accs, err := c.store.List(context.Background(), account.ListFilter{})
	require.NoError(t, err)
	require.Len(t, accs, 1)
	assert.Equal(t, "social-label", accs[0].Label)
	assert.Equal(t, "social", accs[0].AuthMethod)
	assert.Equal(t, "rt_test", *accs[0].RefreshToken)
}

func TestAccountAdd_InvalidType(t *testing.T) {
	c, cleanup := setupTestCLI(t)
	defer cleanup()

	err := c.runAccountAdd(context.Background(), "invalid", "label", "", "ksk_test", "", "us-east-1", "", "", "", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid auth type")
}

func TestAccountList(t *testing.T) {
	c, cleanup := setupTestCLI(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, c.runAccountAdd(ctx, "apikey", "a1", "", "ksk_1", "", "us-east-1", "", "", "", "", ""))
	require.NoError(t, c.runAccountAdd(ctx, "apikey", "a2", "", "ksk_2", "", "us-east-1", "", "", "", "", ""))

	c.jsonOutput = true
	err := c.runAccountList(ctx, false, "")
	require.NoError(t, err)
}

func TestAccountGet(t *testing.T) {
	c, cleanup := setupTestCLI(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, c.runAccountAdd(ctx, "apikey", "get-test", "", "ksk_get", "", "us-east-1", "", "", "", "", ""))
	accs, _ := c.store.List(ctx, account.ListFilter{})
	require.Len(t, accs, 1)

	c.jsonOutput = true
	err := c.runAccountGet(ctx, accs[0].ID)
	require.NoError(t, err)
}

func TestAccountGet_NotFound(t *testing.T) {
	c, cleanup := setupTestCLI(t)
	defer cleanup()

	err := c.runAccountGet(context.Background(), "nonexistent")
	require.Error(t, err)
}

func TestAccountRemove(t *testing.T) {
	c, cleanup := setupTestCLI(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, c.runAccountAdd(ctx, "apikey", "rm-test", "", "ksk_rm", "", "us-east-1", "", "", "", "", ""))
	accs, _ := c.store.List(ctx, account.ListFilter{})
	require.Len(t, accs, 1)

	err := c.runAccountRemove(ctx, accs[0].ID, true)
	require.NoError(t, err)

	accs, err = c.store.List(ctx, account.ListFilter{})
	require.NoError(t, err)
	require.Len(t, accs, 0)
}

func TestAccountEnableDisable(t *testing.T) {
	c, cleanup := setupTestCLI(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, c.runAccountAdd(ctx, "apikey", "en-test", "", "ksk_en", "", "us-east-1", "", "", "", "", ""))
	accs, _ := c.store.List(ctx, account.ListFilter{})
	require.Len(t, accs, 1)
	id := accs[0].ID

	reason := "maintenance"
	require.NoError(t, c.runAccountEnable(ctx, id, false, &reason))

	acc, err := c.store.Get(ctx, id)
	require.NoError(t, err)
	assert.False(t, acc.Enabled)
	assert.Equal(t, "maintenance", *acc.DisabledReason)

	require.NoError(t, c.runAccountEnable(ctx, id, true, nil))
	acc, err = c.store.Get(ctx, id)
	require.NoError(t, err)
	assert.True(t, acc.Enabled)
}

func TestServerCmd(t *testing.T) {
	c, _ := setupTestCLI(t)
	cmd := c.newServerCmd()
	cmd.SetOut(os.Stdout)
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRootCmd_ReturnsCmd(t *testing.T) {
	root, cleanup := NewRootCmd()
	defer cleanup()
	assert.NotNil(t, root)
	assert.Equal(t, "kiro-let-go-cli", root.Use)
}
