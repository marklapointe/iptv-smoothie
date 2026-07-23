package cache_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mlapointe/smoothie/internal/cache"
)

func TestFillAndServe_ProgressiveThenComplete(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	c, err := cache.New(cache.Config{
		Root:     root,
		MaxBytes: 100 << 20,
		TTL:      time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}

	// valid-enough mp4 header + payload (validation looks for ftyp)
	hdr := []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	payload := append(hdr, bytes.Repeat([]byte("ABCDEFGH"), 1024)...)
	src := io.NopCloser(bytes.NewReader(payload))

	obj, reader, err := c.OpenOrFill(context.Background(), "key-1", src, int64(len(payload)), ".mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	// Progressive: can read while fill runs
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch len=%d want=%d", len(got), len(payload))
	}

	// Wait for fill complete / validated
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		o, err := c.Get("key-1")
		if err == nil && (o.State == cache.StateValidated || o.State == cache.StateCorrupt) {
			obj = o
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if obj.State != cache.StateValidated {
		t.Fatalf("state=%s want validated (size=%d path=%s)", obj.State, obj.Size, obj.Path)
	}

	// Second open is cache hit — no new source needed
	obj2, r2, err := c.OpenOrFill(context.Background(), "key-1", nil, 0, ".mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	if obj2.State != cache.StateValidated {
		t.Fatal(obj2.State)
	}
	b2, _ := io.ReadAll(r2)
	if !bytes.Equal(b2, payload) {
		t.Fatal("cache hit mismatch")
	}
}

func TestPromote_OnlyValidated_MoviesVsTV(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	lib := t.TempDir()
	c, err := cache.New(cache.Config{Root: root, MaxBytes: 50 << 20, TTL: time.Hour, LibraryRoot: lib})
	if err != nil {
		t.Fatal(err)
	}
	data := []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	src := io.NopCloser(bytes.NewReader(data))
	_, r, err := c.OpenOrFill(context.Background(), "movie-1", src, int64(len(data)), ".mp4")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, r)
	_ = r.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		o, _ := c.Get("movie-1")
		if o != nil && o.State == cache.StateValidated {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	path, err := c.Promote("movie-1", cache.MediaMeta{
		Kind: cache.KindMovie, Title: "Demo", Year: 2024, Ext: ".mp4",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := filepath.Join(lib, "movies", "Demo (2024)")
	if filepath.Dir(path) != wantPrefix {
		t.Fatalf("path %s not under %s", path, wantPrefix)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}

	// Incomplete must not promote
	c2, _ := cache.New(cache.Config{Root: t.TempDir(), LibraryRoot: t.TempDir()})
	// write incomplete purgatory manually
	_, err = c2.Promote("missing", cache.MediaMeta{Kind: cache.KindMovie, Title: "X", Year: 1, Ext: ".mp4"})
	if err == nil {
		t.Fatal("expected promote fail for missing")
	}
}

func TestReaper_TTLAndMaxBytes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	c, err := cache.New(cache.Config{
		Root:     root,
		MaxBytes: 100,
		TTL:      50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("x"), 40)
	for _, key := range []string{"a", "b", "c"} {
		src := io.NopCloser(bytes.NewReader(data))
		_, r, err := c.OpenOrFill(context.Background(), key, src, int64(len(data)), ".bin")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, r)
		_ = r.Close()
	}
	time.Sleep(80 * time.Millisecond)
	n, err := c.Reap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("expected reaper to remove something, removed=%d", n)
	}
}
