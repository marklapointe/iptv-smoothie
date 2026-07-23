package store_test

import (
	"path/filepath"
	"testing"

	"github.com/mlapointe/smoothie/internal/store"
)

func TestOpen_CreatesSchemaAndPersistsSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "smoothie.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	src := store.Source{
		Name:     "Provider A",
		Type:     store.SourceTypeIPTVM3U,
		Enabled:  true,
		Priority: 10,
		ConfigJSON: `{"urls":["http://example.test/get.php"]}`,
		LimitsJSON: `{"max_concurrent_upstreams":2,"max_upstream_bps":1500000}`,
	}
	if err := db.CreateSource(&src); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if src.ID == "" {
		t.Fatal("expected non-empty source ID")
	}

	// Re-open to prove SQLite persistence via ORM
	if err := db.Close(); err != nil {
		t.Fatalf("Close before reopen: %v", err)
	}
	db2, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })

	got, err := db2.GetSource(src.ID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if got.Name != "Provider A" {
		t.Errorf("Name = %q, want Provider A", got.Name)
	}
	if got.Type != store.SourceTypeIPTVM3U {
		t.Errorf("Type = %q, want %q", got.Type, store.SourceTypeIPTVM3U)
	}
	if !got.Enabled {
		t.Error("Enabled = false, want true")
	}
	if got.Priority != 10 {
		t.Errorf("Priority = %d, want 10", got.Priority)
	}
}

func TestListSources_MultipleSameType(t *testing.T) {
	t.Parallel()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, name := range []string{"IPTV-1", "IPTV-2", "HDHR-1"} {
		typ := store.SourceTypeIPTVM3U
		if name == "HDHR-1" {
			typ = store.SourceTypeHDHomeRun
		}
		if err := db.CreateSource(&store.Source{Name: name, Type: typ, Enabled: true}); err != nil {
			t.Fatalf("CreateSource %s: %v", name, err)
		}
	}

	all, err := db.ListSources()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("len(ListSources) = %d, want 3", len(all))
	}

	iptv, err := db.ListSourcesByType(store.SourceTypeIPTVM3U)
	if err != nil {
		t.Fatal(err)
	}
	if len(iptv) != 2 {
		t.Fatalf("IPTV sources = %d, want 2", len(iptv))
	}
}

func TestChannel_BelongsToSource(t *testing.T) {
	t.Parallel()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	src := store.Source{Name: "A", Type: store.SourceTypeIPTVM3U, Enabled: true}
	if err := db.CreateSource(&src); err != nil {
		t.Fatal(err)
	}
	ch := store.Channel{
		SourceID:   src.ID,
		RemoteKey:  "cnn-hd",
		Name:       "CNN HD",
		GroupName:  "News",
		Kind:       store.ChannelKindLive,
		StreamURL:  "http://example.test/live/cnn",
		Enabled:    true,
	}
	if err := db.CreateChannel(&ch); err != nil {
		t.Fatal(err)
	}
	if ch.ID == "" {
		t.Fatal("expected channel ID")
	}

	list, err := db.ListChannelsBySource(src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "CNN HD" {
		t.Fatalf("unexpected channels: %+v", list)
	}
}

func TestSetting_GetSet(t *testing.T) {
	t.Parallel()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.SetSetting("cache.max_bytes", "214748364800"); err != nil {
		t.Fatal(err)
	}
	v, err := db.GetSetting("cache.max_bytes")
	if err != nil {
		t.Fatal(err)
	}
	if v != "214748364800" {
		t.Errorf("got %q", v)
	}
	if _, err := db.GetSetting("missing"); err == nil {
		t.Fatal("expected error for missing setting")
	}
}
