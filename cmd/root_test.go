package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/aauren/rtorrent-exporter/pkg/rtorrentexporter/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validTestConfig is a config that passes validation, so each case only has to describe what it breaks
func validTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Rtorrent.Addr = "http://localhost:8000/RPC2"
	cfg.Rtorrent.Timeout = 10 * time.Second
	cfg.Rtorrent.Downloads.Collect.Details = true
	cfg.Rtorrent.Trackers.Enabled = true
	cfg.Rtorrent.Trackers.Cache.MinAge = 10 * time.Minute
	cfg.Rtorrent.Trackers.Cache.MaxAge = time.Hour
	cfg.Rtorrent.Trackers.Cache.MaxParallelRequests = 5
	cfg.Telemetry.Addr = ":9135"
	cfg.Telemetry.Path = "/metrics"
	cfg.Telemetry.Timeout = 10 * time.Second
	return cfg
}

const shutdownHelperEnv = "RTORRENT_EXPORTER_TEST_SHUTDOWN_HELPER"

// This re-execs the test binary as a child that installs the signal handler the same way RunRoot does and then pretends to be stuck in a
// slow drain. The parent sends SIGINT twice, and the second one has to kill the child rather than being swallowed by the handler.
func TestWaitForShutdown_secondSignalKills(t *testing.T) {
	if os.Getenv(shutdownHelperEnv) == "1" {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT)
		fmt.Println("ready")
		waitForShutdown(ctx, stop)
		fmt.Println("draining")
		time.Sleep(10 * time.Second)
		return
	}

	//nolint:gosec // G204 doesn't like os.Args[0], but re-running our own test binary is the whole point
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestWaitForShutdown_secondSignalKills$")
	cmd.Env = append(os.Environ(), shutdownHelperEnv+"=1")
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	lines := bufio.NewScanner(stdout)
	waitForLine := func(want string) {
		t.Helper()
		for lines.Scan() {
			if lines.Text() == want {
				return
			}
		}
		t.Fatalf("child exited before printing %q", want)
	}

	waitForLine("ready")
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	waitForLine("draining")
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)
		status, ok := exitErr.Sys().(syscall.WaitStatus)
		require.True(t, ok)
		assert.Equal(t, syscall.SIGINT, status.Signal(), "expected the second SIGINT to kill the child")
	case <-time.After(2 * time.Second):
		t.Fatal("child swallowed the second SIGINT and is still draining")
	}
}

func TestStartupSummary(t *testing.T) {
	t.Parallel()
	summary := startupSummary(validTestConfig())

	// Every parenthesised group should be separated by a space, a ")(" means one of the format strings lost its trailing space
	assert.NotContains(t, summary, ")(")
	assert.Contains(t, summary, "(collect tracker info: true)")
	// The verbs and the args drifted out of step at some point, so pin a few that were landing in the wrong slot
	assert.Contains(t, summary, `on ":9135/metrics"`)
	assert.Contains(t, summary, `for server "http://localhost:8000/RPC2"`)
	assert.Contains(t, summary, "(insecure: false)")
}

func TestFlagUsageSpelling(t *testing.T) {
	t.Parallel()
	usage := rootCmd.Flags().Lookup("rtorrent.insecure").Usage

	assert.NotContains(t, usage, "certificat ")
	assert.Contains(t, usage, "certificate")
}

func TestLoadConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		useViper bool
		wantAddr string
	}{
		{"unmarshals when useviper is set", true, "http://from-viper"},
		{"leaves config alone when useviper is unset", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := viper.New()
			v.Set("useviper", tt.useViper)
			v.Set("rtorrent.addr", "http://from-viper")
			cfg := &config.Config{}

			require.NoError(t, loadConfig(v, cfg))
			assert.Equal(t, tt.wantAddr, cfg.Rtorrent.Addr)
		})
	}
}

// Nested keys have dots and dashes in them, neither of which most shells will let you put in an env var name
func TestConfigureViperEnv(t *testing.T) {
	tests := []struct {
		name string
		env  string
		key  string
	}{
		{"top level key", "RTORRENT_EXPORTER_USEVIPER", "useviper"},
		{"nested key", "RTORRENT_EXPORTER_RTORRENT_ADDR", "rtorrent.addr"},
		{"nested key with dash", "RTORRENT_EXPORTER_RTORRENT_TRACKERS_CACHE_MIN_AGE", "rtorrent.trackers.cache.min-age"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			configureViperEnv(v)
			t.Setenv(tt.env, "from-env")

			assert.Equal(t, "from-env", v.GetString(tt.key))
		})
	}
}

func TestValidateConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(cfg *config.Config)
		wantErr bool
	}{
		{"valid", func(*config.Config) {}, false},
		{"min-age equal to max-age", func(cfg *config.Config) {
			cfg.Rtorrent.Trackers.Cache.MinAge = cfg.Rtorrent.Trackers.Cache.MaxAge
		}, true},
		{"min-age greater than max-age", func(cfg *config.Config) {
			cfg.Rtorrent.Trackers.Cache.MinAge = 2 * cfg.Rtorrent.Trackers.Cache.MaxAge
		}, true},
		{"min-age greater than max-age with trackers disabled", func(cfg *config.Config) {
			cfg.Rtorrent.Trackers.Enabled = false
			cfg.Rtorrent.Trackers.Cache.MinAge = 2 * cfg.Rtorrent.Trackers.Cache.MaxAge
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := validTestConfig()
			tt.mutate(cfg)

			err := validateConfig(cfg)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
