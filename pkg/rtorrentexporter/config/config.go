package config

import (
	"regexp"
	"time"
)

// Config represents the configuration of the rtorrent-exporter.
type Config struct {
	Rtorrent  RtorrentConfig  `mapstructure:"rtorrent"`
	Telemetry TelemetryConfig `mapstructure:"telemetry"`
	UseViper  bool            `mapstructure:"useviper"`
}

// RtorrentConfig is the configuration for connecting to rtorrent and controlling which metrics we collect.
type RtorrentConfig struct {
	Addr      string          `mapstructure:"addr"`
	Downloads DownloadsConfig `mapstructure:"downloads"`
	Insecure  bool            `mapstructure:"insecure"`
	Password  string          `mapstructure:"password"`
	Timeout   time.Duration   `mapstructure:"timeout"`
	Trackers  TrackersConfig  `mapstructure:"trackers"`
	Username  string          `mapstructure:"username"`
}

// DownloadsConfig is the configuration for controlling which download metrics we collect.
type DownloadsConfig struct {
	Collect CollectConfig `mapstructure:"collect"`
}

// CollectConfig is the configuration for controlling which download metrics we collect.
type CollectConfig struct {
	Details  bool `mapstructure:"details"`
	Messages bool `mapstructure:"messages"`
}

// TrackersConfig is the configuration for controlling whether we collect tracker metrics and how we handle tracker names.
type TrackersConfig struct {
	Cache                    CacheConfig                `mapstructure:"cache"`
	Enabled                  bool                       `mapstructure:"enabled"`
	TrackerNameSubstitutions []TrackerNameSubstitutions `mapstructure:"tracker-name-substitutions"`
}

// TrackerNameSubstitutions is a configuration for converting tracker names that match various regex expressions to a new name.
type TrackerNameSubstitutions struct {
	ConvertTo       string           `mapstructure:"convert-to"`
	Matchers        []string         `mapstructure:"matchers"`
	CompiledMathers []*regexp.Regexp `mapstructure:"-"`
}

// CacheConfig is the configuration for controlling how we cache tracker information for emitting metrics
type CacheConfig struct {
	MaxAge              time.Duration `mapstructure:"max-age"`
	MaxParallelRequests int           `mapstructure:"max-parallel-requests"`
	MinAge              time.Duration `mapstructure:"min-age"`
}

// TelemetryConfig is the configuration for the telemetry server.
type TelemetryConfig struct {
	Addr        string        `mapstructure:"addr"`
	Path        string        `mapstructure:"path"`
	Timeout     time.Duration `mapstructure:"timeout"`
	EnablePProf bool          `mapstructure:"enable-pprof"`
}
