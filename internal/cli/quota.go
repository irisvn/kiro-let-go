package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/spf13/cobra"
)

type quotaDisplay struct {
	SubscriptionTitle string    `json:"subscription_title"`
	LimitTotal        int64     `json:"limit_total"`
	LimitRemaining    int64     `json:"limit_remaining"`
	ResetTime         time.Time `json:"reset_time"`
	FetchedAt         time.Time `json:"fetched_at"`
}

func (c *CLI) newQuotaCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "quota [account-id]",
		Short:   "Show quota information",
		PreRunE: func(cmd *cobra.Command, args []string) error { return c.init(cmd.Context()) },
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return c.runQuotaAll(cmd.Context(), force)
			}
			return c.runQuotaSingle(cmd.Context(), args[0], force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "bypass cache and fetch upstream")
	return cmd
}

func (c *CLI) runQuotaAll(ctx context.Context, force bool) error {
	if !force {
		items, err := c.fetcher.Summary(ctx)
		if err != nil {
			return fmt.Errorf("quota summary: %w", err)
		}
		return c.outputQuotaSummary(items)
	}

	accounts, err := c.store.List(ctx, account.ListFilter{})
	if err != nil {
		return fmt.Errorf("list accounts: %w", err)
	}

	type item struct {
		AccountID string        `json:"account_id"`
		Label     string        `json:"label"`
		Enabled   bool          `json:"enabled"`
		Quota     *quotaDisplay `json:"quota,omitempty"`
		Error     string        `json:"error,omitempty"`
	}

	out := make([]item, 0, len(accounts))
	for i := range accounts {
		acc := &accounts[i]
		it := item{AccountID: acc.ID, Label: acc.Label, Enabled: acc.Enabled}
		q, err := c.fetcher.Get(ctx, acc, true)
		if err != nil {
			it.Error = err.Error()
		} else {
			it.Quota = toQuotaDisplay(q)
		}
		out = append(out, it)
	}

	if c.jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ACCOUNT_ID\tLABEL\tENABLED\tSUBSCRIPTION\tTOTAL\tREMAINING\tRESET_TIME\tFETCHED_AT\tERROR")
	for _, it := range out {
		if it.Quota != nil {
			fmt.Fprintf(w, "%s\t%s\t%v\t%s\t%d\t%d\t%s\t%s\t\n",
				it.AccountID, it.Label, it.Enabled, it.Quota.SubscriptionTitle,
				it.Quota.LimitTotal, it.Quota.LimitRemaining,
				formatTime(it.Quota.ResetTime), formatTime(it.Quota.FetchedAt))
		} else {
			fmt.Fprintf(w, "%s\t%s\t%v\t\t\t\t\t\t%s\n",
				it.AccountID, it.Label, it.Enabled, it.Error)
		}
	}
	return w.Flush()
}

func (c *CLI) runQuotaSingle(ctx context.Context, id string, force bool) error {
	acc, err := c.store.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("get account: %w", err)
	}
	q, err := c.fetcher.Get(ctx, acc, force)
	if err != nil {
		return fmt.Errorf("fetch quota: %w", err)
	}
	disp := toQuotaDisplay(q)
	if c.jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(disp)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "SubscriptionTitle:\t%s\n", disp.SubscriptionTitle)
	fmt.Fprintf(w, "LimitTotal:\t%d\n", disp.LimitTotal)
	fmt.Fprintf(w, "LimitRemaining:\t%d\n", disp.LimitRemaining)
	fmt.Fprintf(w, "ResetTime:\t%s\n", formatTime(disp.ResetTime))
	fmt.Fprintf(w, "FetchedAt:\t%s\n", formatTime(disp.FetchedAt))
	return w.Flush()
}

func (c *CLI) outputQuotaSummary(items []*account.QuotaSummaryItem) error {
	type outItem struct {
		AccountID string        `json:"account_id"`
		Label     string        `json:"label"`
		Enabled   bool          `json:"enabled"`
		Quota     *quotaDisplay `json:"quota,omitempty"`
	}

	out := make([]outItem, 0, len(items))
	for _, it := range items {
		o := outItem{AccountID: it.AccountID, Label: it.Label, Enabled: it.Enabled}
		if it.Quota != nil {
			o.Quota = toQuotaDisplay(it.Quota)
		}
		out = append(out, o)
	}

	if c.jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ACCOUNT_ID\tLABEL\tENABLED\tSUBSCRIPTION\tTOTAL\tREMAINING\tRESET_TIME\tFETCHED_AT")
	for _, o := range out {
		if o.Quota != nil {
			fmt.Fprintf(w, "%s\t%s\t%v\t%s\t%d\t%d\t%s\t%s\n",
				o.AccountID, o.Label, o.Enabled, o.Quota.SubscriptionTitle,
				o.Quota.LimitTotal, o.Quota.LimitRemaining,
				formatTime(o.Quota.ResetTime), formatTime(o.Quota.FetchedAt))
		} else {
			fmt.Fprintf(w, "%s\t%s\t%v\t\t\t\t\t\n", o.AccountID, o.Label, o.Enabled)
		}
	}
	return w.Flush()
}

func toQuotaDisplay(q *account.Quota) *quotaDisplay {
	if q == nil {
		return nil
	}
	return &quotaDisplay{
		SubscriptionTitle: q.SubscriptionTitle,
		LimitTotal:        q.LimitTotal,
		LimitRemaining:    q.LimitRemaining,
		ResetTime:         q.ResetTime,
		FetchedAt:         q.FetchedAt,
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
