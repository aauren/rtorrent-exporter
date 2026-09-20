package cmd

import (
	"testing"
	"time"

	"github.com/aauren/rtorrent-exporter/pkg/rtorrentexporter/config"
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
