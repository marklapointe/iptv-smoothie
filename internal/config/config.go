package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config is process-level configuration (file/env). Runtime settings live in SQLite.
type Config struct {
	ListenAddr string
	DataDir    string
	DBPath     string
	StaticDir  string
}

// Default returns sensible defaults for local/dev.
func Default() Config {
	data := filepath.Join(".", "data")
	return Config{
		ListenAddr: envOr("SMOOTHIE_LISTEN", "127.0.0.1:8787"),
		DataDir:    envOr("SMOOTHIE_DATA_DIR", data),
		DBPath:     envOr("SMOOTHIE_DB", filepath.Join(data, "smoothie.db")),
		StaticDir:  envOr("SMOOTHIE_STATIC", filepath.Join("web", "dist")),
	}
}

// EnsureDataDir creates the data directory if missing.
func (c Config) EnsureDataDir() error {
	if c.DataDir == "" {
		return fmt.Errorf("config: empty data dir")
	}
	return os.MkdirAll(c.DataDir, 0o750)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
