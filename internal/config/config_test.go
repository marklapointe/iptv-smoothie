package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mlapointe/smoothie/internal/config"
)

func TestDefault_ListenAndData(t *testing.T) {
	t.Setenv("SMOOTHIE_LISTEN", "")
	t.Setenv("SMOOTHIE_DATA_DIR", "")
	t.Setenv("SMOOTHIE_DB", "")
	c := config.Default()
	if c.ListenAddr != "127.0.0.1:8787" {
		t.Errorf("ListenAddr = %q", c.ListenAddr)
	}
	if c.DataDir == "" || c.DBPath == "" {
		t.Fatalf("empty paths: %+v", c)
	}
}

func TestEnsureDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	c := config.Config{DataDir: dir}
	if err := c.EnsureDataDir(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}
}
