package hub_test

import (
	"bytes"
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mlapointe/smoothie/internal/hub"
)

func TestHub_SingleUpstreamMultipleClients(t *testing.T) {
	t.Parallel()
	payload := bytes.Repeat([]byte("TSFRAME-"), 256)

	var upstreamOpens atomic.Int64
	// Gate: do not finish upstream until all subscribers are reading
	var started sync.WaitGroup
	started.Add(3)

	h := hub.New(hub.Options{IdleGrace: 20 * time.Millisecond})

	open := func() (io.ReadCloser, error) {
		upstreamOpens.Add(1)
		r, w := io.Pipe()
		go func() {
			// wait briefly for subscribers to attach
			started.Wait()
			_, _ = w.Write(payload)
			_ = w.Close()
		}()
		return r, nil
	}

	var wg sync.WaitGroup
	results := make([][]byte, 3)
	errs := make([]error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := h.Subscribe(context.Background(), "ch-1", open)
			if err != nil {
				errs[i] = err
				started.Done()
				return
			}
			defer r.Close()
			started.Done()
			b, err := io.ReadAll(r)
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = b
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("client %d: %v", i, err)
		}
	}
	if upstreamOpens.Load() != 1 {
		t.Fatalf("upstream opens = %d, want 1", upstreamOpens.Load())
	}
	for i, b := range results {
		if !bytes.Equal(b, payload) {
			t.Fatalf("client %d payload mismatch (len=%d want %d)", i, len(b), len(payload))
		}
	}
}
