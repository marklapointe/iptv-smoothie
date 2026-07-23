package stream_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mlapointe/smoothie/internal/store"
	"github.com/mlapointe/smoothie/internal/stream"
)

func TestOpenLive_SingleUpstreamMultipleClients(t *testing.T) {
	t.Parallel()
	var opens atomic.Int64
	payload := []byte("LIVE-TS-PAYLOAD-DATA-LIVE-TS-PAYLOAD")

	// Upstream writes immediately (no gate). Late joiners may miss data;
	// both clients Subscribe before any Read so both attach before pump drains
	// a small payload if we slow the write slightly.
	startWrite := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		opens.Add(1)
		w.Header().Set("Content-Type", "video/mp2t")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-startWrite
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	db, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	src := store.Source{
		Name: "T", Type: store.SourceTypeIPTVM3U, Enabled: true,
		ConfigJSON: `{"urls":["http://x"]}`,
		LimitsJSON: `{"max_concurrent_upstreams":2}`,
	}
	if err := db.CreateSource(&src); err != nil {
		t.Fatal(err)
	}
	ch := store.Channel{
		SourceID: src.ID, RemoteKey: "live1", Name: "Live 1",
		Kind: store.ChannelKindLive, StreamURL: srv.URL + "/live/1", Enabled: true,
	}
	if err := db.CreateChannel(&ch); err != nil {
		t.Fatal(err)
	}

	m := stream.New(db)
	m.Client = srv.Client()

	// Sequential subscribe so both join before data flows
	r1, err := m.OpenLive(context.Background(), &ch)
	if err != nil {
		t.Fatal(err)
	}
	defer r1.Close()
	r2, err := m.OpenLive(context.Background(), &ch)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()

	close(startWrite)

	var wg sync.WaitGroup
	got := make([][]byte, 2)
	wg.Add(2)
	go func() { defer wg.Done(); b, _ := io.ReadAll(r1); got[0] = b }()
	go func() { defer wg.Done(); b, _ := io.ReadAll(r2); got[1] = b }()
	wg.Wait()

	if opens.Load() != 1 {
		t.Fatalf("upstream opens = %d, want 1", opens.Load())
	}
	for i, b := range got {
		if string(b) != string(payload) {
			t.Fatalf("client %d got %q", i, b)
		}
	}
}

func TestOpenLive_PoolExhausted(t *testing.T) {
	t.Parallel()
	// Hold first upstream open after headers
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	db, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	src := store.Source{
		Name: "T", Type: store.SourceTypeIPTVM3U, Enabled: true,
		ConfigJSON: `{"urls":["http://x"]}`,
		LimitsJSON: `{"max_concurrent_upstreams":1}`,
	}
	if err := db.CreateSource(&src); err != nil {
		t.Fatal(err)
	}
	mk := func(name, path string) store.Channel {
		ch := store.Channel{
			SourceID: src.ID, RemoteKey: name, Name: name, Kind: store.ChannelKindLive,
			StreamURL: srv.URL + path, Enabled: true,
		}
		if err := db.CreateChannel(&ch); err != nil {
			t.Fatal(err)
		}
		return ch
	}
	ch1 := mk("a", "/a")
	ch2 := mk("b", "/b")

	m := stream.New(db)
	m.Client = srv.Client()

	r1, err := m.OpenLive(context.Background(), &ch1)
	if err != nil {
		t.Fatal(err)
	}
	defer r1.Close()
	time.Sleep(50 * time.Millisecond)

	ctx2, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err = m.OpenLive(ctx2, &ch2)
	if err == nil {
		t.Fatal("expected second distinct live channel to fail when pool=1")
	}
}
