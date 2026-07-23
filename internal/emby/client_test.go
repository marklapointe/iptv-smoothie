package emby_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mlapointe/smoothie/internal/emby"
)

func TestClient_RefreshAndFolders(t *testing.T) {
	t.Parallel()
	var refreshed bool
	mux := http.NewServeMux()
	mux.HandleFunc("/System/Info", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Emby-Token") == "" && r.URL.Query().Get("api_key") == "" {
			http.Error(w, "no key", 401)
			return
		}
		_, _ = w.Write([]byte(`{"ServerName":"test"}`))
	})
	mux.HandleFunc("/Library/MediaFolders", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Items": []map[string]string{
				{"Name": "Movies", "Id": "1", "Path": "/media/movies"},
				{"Name": "TV", "Id": "2", "Path": "/media/tv"},
			},
		})
	})
	mux.HandleFunc("/Library/Refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		refreshed = true
		w.WriteHeader(204)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := emby.New(srv.URL, "secret-key")
	c.HTTP = srv.Client()
	if err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	folders, err := c.MediaFolders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 2 {
		t.Fatalf("folders=%d", len(folders))
	}
	if err := c.RefreshLibrary(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !refreshed {
		t.Fatal("expected refresh called")
	}
}
