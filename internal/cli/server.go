package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (c *CLI) newServerCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "server",
		Short:             "Server convenience alias",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "Use kiro-let-go server instead")
		},
	}
}
