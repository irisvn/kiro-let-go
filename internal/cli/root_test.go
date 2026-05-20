package cli

import (
	"bytes"
	"testing"

	"github.com/irisvn/kiro-let-go/internal/version"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestNewRootCmdVersion(t *testing.T) {
	cmd, cleanup := NewRootCmd()
	defer cleanup()

	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"version"})

	require.NoError(t, cmd.Execute())
	require.Equal(t, version.Version+"\n", buf.String())
}

func TestValidateCommandTreeRejectsReservedName(t *testing.T) {
	cmd := &cobra.Command{Use: "help"}
	require.Error(t, validateCommandTree(cmd))
}
