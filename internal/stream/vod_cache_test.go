package stream_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/mlapointe/smoothie/internal/cache"
	"github.com/mlapointe/smoothie/internal/store"
	"github.com/mlapointe/smoothie/internal/stream"
)

func TestOpenVOD_CacheHitSkipsSecondUpstream(t *testing.T) {
	t.Parallel()
	var opens atomic.Int64
	// valid-ish mp4
	payload := []byte{
		0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm',
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	}
	payload = append(payload, make([]byte, 1024)...)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		opens.Add(1)
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", "1048")
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
		LimitsJSON: `{"max_concurrent_upstreams":2,"max_upstream_bps":0}`,
	}
	if err := db.CreateSource(&src); err != nil {
		t.Fatal(err)
	}
	ch := store.Channel{
		SourceID: src.ID, RemoteKey: "m1", Name: "Movie", Kind: store.ChannelKindVOD,
		StreamURL: srv.URL + "/movie/1.mp4", Enabled: true,
	}
	if err := db.CreateChannel(&ch); err != nil {
		t.Fatal(err)
	}

	cch, err := cache.New(cache.Config{Root: t.TempDir(), MaxBytes: 50 << 20})
	if err != nil {
		t.Fatal(err)
	}
	m := stream.New(db, stream.Options{Cache: cch})
	m.Client = srv.Client()

	r1, err := m.OpenVOD(context.Background(), &ch, "")
	if err != nil {
		t.Fatal(err)
	}
	b1, err := io.ReadAll(r1.Body)
	_ = r1.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(b1) == 0 {
		t.Fatal("empty body")
	}

	// wait for validation
	key := ch.SourceID + ":" + ch.ID
	for i := 0; i < 100; i++ {
		o, err := cch.Get(key)
		if err == nil && o.State == cache.StateValidated {
			break
		}
	}

	r2, err := m.OpenVOD(context.Background(), &ch, "")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, r2.Body)
	_ = r2.Body.Close()

	if !r2.CacheHit {
		t.Fatal("expected cache hit on second play")
	}
	if opens.Load() != 1 {
		t.Fatalf("upstream opens = %d, want 1", opens.Load())
	}
}
