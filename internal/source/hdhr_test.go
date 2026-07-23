package source_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mlapointe/smoothie/internal/source"
	"github.com/mlapointe/smoothie/internal/store"
)

func TestRefreshHDHR_ImportsLineup(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/discover.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"FriendlyName": "HDHomeRun TEST",
			"DeviceID":     "DEADBEEF",
			"TunerCount":   2,
			"BaseURL":      "http://hdhr.test",
			"LineupURL":    "http://hdhr.test/lineup.json",
		})
	})
	mux.HandleFunc("/lineup.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"GuideNumber": "7.1", "GuideName": "WABC-HD", "URL": "http://hdhr.test:5004/auto/v7.1"},
			{"GuideNumber": "7.2", "GuideName": "WABC-SD", "URL": "http://hdhr.test:5004/auto/v7.2"},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	db, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	src := store.Source{
		Name:       "OTA-1",
		Type:       store.SourceTypeHDHomeRun,
		Enabled:    true,
		ConfigJSON: `{"base_url":"` + srv.URL + `"}`,
	}
	if err := db.CreateSource(&src); err != nil {
		t.Fatal(err)
	}

	ref := source.NewRefresher(db)
	ref.Client = srv.Client()
	res, err := ref.RefreshHDHR(context.Background(), src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 || res.Live != 2 {
		t.Fatalf("result=%+v", res)
	}
	chs, err := db.ListChannelsBySource(src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chs) != 2 {
		t.Fatalf("channels=%d", len(chs))
	}
	if chs[0].Kind != store.ChannelKindLive {
		t.Fatal(chs[0].Kind)
	}
}
