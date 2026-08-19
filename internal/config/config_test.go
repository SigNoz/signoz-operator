package config

import (
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testCmd = func(logLevel *string) *cobra.Command {
	c := &cobra.Command{
		Use: "app",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return Set(cmd, cmd.Use)
		},
		Run: func(cmd *cobra.Command, args []string) {},
	}
	c.Flags().StringVar(logLevel, "log-level", "debug", "The log level")
	return c
}

func TestSetWithFlag(t *testing.T) {
	var logLevel string
	cmd := testCmd(&logLevel)
	cmd.SetArgs([]string{"--log-level=info"})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "info", logLevel)
}

// The *PreRun and *PostRun functions will only be executed if the Run function of the current command has been declared.
func TestSetWithEnv(t *testing.T) {
	t.Setenv("APP_LOG_LEVEL", "warn")

	var logLevel string
	cmd := testCmd(&logLevel)
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "warn", logLevel)
}

func TestSetWithFlagAndEnv(t *testing.T) {
	t.Setenv("APP_LOG_LEVEL", "warn")

	var logLevel string
	cmd := testCmd(&logLevel)
	cmd.SetArgs([]string{"--log-level=info"})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "info", logLevel)
}

func TestSetWithInvalidEnv(t *testing.T) {
	t.Setenv("APP_TIMEOUT", "banana")

	var timeout time.Duration
	cmd := &cobra.Command{
		Use:           "app",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return Set(cmd, cmd.Use)
		},
		Run: func(cmd *cobra.Command, args []string) {},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", time.Second, "The timeout")

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "APP_TIMEOUT")
}
