package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/kiro"
	"github.com/spf13/cobra"
)

type safeAccount struct {
	ID             string     `json:"id"`
	Label          string     `json:"label"`
	AuthMethod     string     `json:"auth_method"`
	Region         string     `json:"region"`
	AuthRegion     *string    `json:"auth_region,omitempty"`
	APIRegion      *string    `json:"api_region,omitempty"`
	ProfileARN     *string    `json:"profile_arn,omitempty"`
	MachineID      string     `json:"machine_id"`
	ProxyURL       *string    `json:"proxy_url,omitempty"`
	ProxyUsername  *string    `json:"proxy_username,omitempty"`
	Enabled        bool       `json:"enabled"`
	DisabledReason *string    `json:"disabled_reason,omitempty"`
	FailureCount   int        `json:"failure_count"`
	LastFailureAt  *time.Time `json:"last_failure_at,omitempty"`
	SuccessCount   int        `json:"success_count"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	CircuitOpen    bool       `json:"circuit_open,omitempty"`
	CircuitReason  string     `json:"circuit_reason,omitempty"`
}

func (c *CLI) newAccountCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "account", Short: "Manage accounts"}
	cmd.AddCommand(c.newAccountAddCmd())
	cmd.AddCommand(c.newAccountListCmd())
	cmd.AddCommand(c.newAccountGetCmd())
	cmd.AddCommand(c.newAccountRemoveCmd())
	cmd.AddCommand(c.newAccountEnableCmd())
	cmd.AddCommand(c.newAccountDisableCmd())
	cmd.AddCommand(c.newAccountRefreshCmd())
	return cmd
}

func (c *CLI) newAccountAddCmd() *cobra.Command {
	var (
		authType      string
		label         string
		refreshToken  string
		apiKey        string
		profileARN    string
		region        string
		authRegion    string
		apiRegion     string
		proxyURL      string
		proxyUsername string
		proxyPassword string
	)
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a new account",
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runAccountAdd(cmd.Context(), authType, label, refreshToken, apiKey, profileARN, region, authRegion, apiRegion, proxyURL, proxyUsername, proxyPassword)
		},
	}
	cmd.Flags().StringVar(&authType, "type", "", "auth type: social|apikey")
	_ = cmd.MarkFlagRequired("type")
	cmd.Flags().StringVar(&label, "label", "", "account label")
	_ = cmd.MarkFlagRequired("label")
	cmd.Flags().StringVar(&refreshToken, "refresh-token", "", "refresh token (social)")
	cmd.Flags().StringVar(&apiKey, "key", "", "API key (apikey)")
	cmd.Flags().StringVar(&profileARN, "profile-arn", "", "profile ARN")
	cmd.Flags().StringVar(&region, "region", "us-east-1", "region")
	cmd.Flags().StringVar(&authRegion, "auth-region", "", "auth region")
	cmd.Flags().StringVar(&apiRegion, "api-region", "", "API region")
	cmd.Flags().StringVar(&proxyURL, "proxy-url", "", "proxy URL")
	cmd.Flags().StringVar(&proxyUsername, "proxy-username", "", "proxy username")
	cmd.Flags().StringVar(&proxyPassword, "proxy-password", "", "proxy password")
	return cmd
}

func (c *CLI) runAccountAdd(ctx context.Context, authType, label, refreshToken, apiKey, profileARN, region, authRegion, apiRegion, proxyURL, proxyUsername, proxyPassword string) error {
	acc := &account.Account{
		ID:         uuid.New().String(),
		Label:      label,
		AuthMethod: authType,
		Region:     region,
		Enabled:    true,
	}
	acc.MachineID = kiro.Generate(acc.ID)

	if authRegion != "" {
		acc.AuthRegion = &authRegion
	}
	if apiRegion != "" {
		acc.APIRegion = &apiRegion
	}
	if profileARN != "" {
		acc.ProfileARN = &profileARN
	}
	if proxyURL != "" {
		acc.ProxyURL = &proxyURL
	}
	if proxyUsername != "" {
		acc.ProxyUsername = &proxyUsername
	}
	if proxyPassword != "" {
		acc.ProxyPassword = &proxyPassword
	}

	switch strings.ToLower(authType) {
	case "social":
		if refreshToken == "" {
			return fmt.Errorf("refresh-token is required for social auth")
		}
		acc.RefreshToken = &refreshToken
	case "apikey":
		if apiKey == "" {
			return fmt.Errorf("key is required for apikey auth")
		}
		acc.APIKey = &apiKey
	default:
		return fmt.Errorf("invalid auth type: %s", authType)
	}

	if err := c.store.Create(ctx, acc); err != nil {
		return fmt.Errorf("create account: %w", err)
	}

	if strings.ToLower(authType) == "social" {
		socialAuth := kiro.NewSocialAuth(nil, c.logger)
		access, refresh, expiresAt, err := socialAuth.Refresh(ctx, acc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: immediate refresh failed: %v\n", err)
		} else {
			acc.AccessToken = &access
			if refresh != "" {
				acc.RefreshToken = &refresh
			}
			acc.ExpiresAt = &expiresAt
			if err := c.store.Update(ctx, acc); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to save refreshed tokens: %v\n", err)
			}
		}
	}

	return c.outputAccount(c.toSafeAccount(acc))
}

func (c *CLI) newAccountListCmd() *cobra.Command {
	var enabledOnly bool
	var authMethod string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List accounts",
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runAccountList(cmd.Context(), enabledOnly, authMethod)
		},
	}
	cmd.Flags().BoolVar(&enabledOnly, "enabled-only", false, "show only enabled accounts")
	cmd.Flags().StringVar(&authMethod, "auth-method", "", "filter by auth method")
	return cmd
}

func (c *CLI) runAccountList(ctx context.Context, enabledOnly bool, authMethod string) error {
	accounts, err := c.store.List(ctx, account.ListFilter{EnabledOnly: enabledOnly, AuthMethod: authMethod})
	if err != nil {
		return fmt.Errorf("list accounts: %w", err)
	}
	out := make([]*safeAccount, 0, len(accounts))
	for i := range accounts {
		out = append(out, c.toSafeAccount(&accounts[i]))
	}
	return c.outputAccounts(out)
}

func (c *CLI) newAccountGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get a single account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runAccountGet(cmd.Context(), args[0])
		},
	}
}

func (c *CLI) runAccountGet(ctx context.Context, id string) error {
	acc, err := c.store.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("get account: %w", err)
	}
	safe := c.toSafeAccount(acc)
	if c.circuit != nil {
		safe.CircuitOpen = c.circuit.IsOpen(id)
		safe.CircuitReason = c.circuit.Reason(id)
	}
	return c.outputAccount(safe)
}

func (c *CLI) newAccountRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove an account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runAccountRemove(cmd.Context(), args[0], yes)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	return cmd
}

func (c *CLI) runAccountRemove(ctx context.Context, id string, yes bool) error {
	if !yes {
		fmt.Fprintf(os.Stderr, "Remove account %s? [y/N]: ", id)
		reader := bufio.NewReader(os.Stdin)
		resp, _ := reader.ReadString('\n')
		resp = strings.ToLower(strings.TrimSpace(resp))
		if resp != "y" && resp != "yes" {
			return fmt.Errorf("cancelled")
		}
	}
	if err := c.store.Delete(ctx, id); err != nil {
		return fmt.Errorf("remove account: %w", err)
	}
	fmt.Println("Account removed")
	return nil
}

func (c *CLI) newAccountEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <id>",
		Short: "Enable an account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runAccountEnable(cmd.Context(), args[0], true, nil)
		},
	}
}

func (c *CLI) newAccountDisableCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "disable <id>",
		Short: "Disable an account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runAccountEnable(cmd.Context(), args[0], false, &reason)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "reason for disabling")
	return cmd
}

func (c *CLI) runAccountEnable(ctx context.Context, id string, enabled bool, reason *string) error {
	if err := c.store.SetEnabled(ctx, id, enabled, reason); err != nil {
		return fmt.Errorf("update account: %w", err)
	}
	if enabled {
		fmt.Println("Account enabled")
	} else {
		fmt.Println("Account disabled")
	}
	return nil
}

func (c *CLI) newAccountRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh <id>",
		Short: "Force token refresh for an account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runAccountRefresh(cmd.Context(), args[0])
		},
	}
}

func (c *CLI) runAccountRefresh(ctx context.Context, id string) error {
	if err := c.manager.Refresh(ctx, id); err != nil {
		return fmt.Errorf("refresh account: %w", err)
	}
	fmt.Println("Account refreshed")
	return nil
}

func (c *CLI) toSafeAccount(acc *account.Account) *safeAccount {
	return &safeAccount{
		ID:             acc.ID,
		Label:          acc.Label,
		AuthMethod:     acc.AuthMethod,
		Region:         acc.Region,
		AuthRegion:     acc.AuthRegion,
		APIRegion:      acc.APIRegion,
		ProfileARN:     acc.ProfileARN,
		MachineID:      acc.MachineID,
		ProxyURL:       acc.ProxyURL,
		ProxyUsername:  acc.ProxyUsername,
		Enabled:        acc.Enabled,
		DisabledReason: acc.DisabledReason,
		FailureCount:   acc.FailureCount,
		LastFailureAt:  acc.LastFailureAt,
		SuccessCount:   acc.SuccessCount,
		LastUsedAt:     acc.LastUsedAt,
		CreatedAt:      acc.CreatedAt,
		UpdatedAt:      acc.UpdatedAt,
	}
}

func (c *CLI) outputAccounts(accs []*safeAccount) error {
	if c.jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(accs)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tLABEL\tAUTH\tENABLED\tREGION\tMACHINE_ID\tFAILURES\tSUCCESSES\tCREATED")
	for _, a := range accs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%v\t%s\t%s\t%d\t%d\t%s\n",
			a.ID, a.Label, a.AuthMethod, a.Enabled, a.Region, a.MachineID,
			a.FailureCount, a.SuccessCount, a.CreatedAt.Format(time.RFC3339))
	}
	return w.Flush()
}

func (c *CLI) outputAccount(acc *safeAccount) error {
	if c.jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(acc)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "ID:\t%s\n", acc.ID)
	fmt.Fprintf(w, "Label:\t%s\n", acc.Label)
	fmt.Fprintf(w, "AuthMethod:\t%s\n", acc.AuthMethod)
	fmt.Fprintf(w, "Enabled:\t%v\n", acc.Enabled)
	fmt.Fprintf(w, "Region:\t%s\n", acc.Region)
	fmt.Fprintf(w, "MachineID:\t%s\n", acc.MachineID)
	fmt.Fprintf(w, "FailureCount:\t%d\n", acc.FailureCount)
	fmt.Fprintf(w, "SuccessCount:\t%d\n", acc.SuccessCount)
	fmt.Fprintf(w, "CreatedAt:\t%s\n", acc.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "UpdatedAt:\t%s\n", acc.UpdatedAt.Format(time.RFC3339))
	if acc.CircuitOpen {
		fmt.Fprintf(w, "CircuitOpen:\t%v\n", acc.CircuitOpen)
		fmt.Fprintf(w, "CircuitReason:\t%s\n", acc.CircuitReason)
	}
	return w.Flush()
}
