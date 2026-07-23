package ratelimit_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mlapointe/smoothie/internal/ratelimit"
)

func TestPool_MaxConcurrent(t *testing.T) {
	t.Parallel()
	p := ratelimit.NewPool(2)
	ctx := context.Background()

	r1, err := p.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := p.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.InUse() != 2 {
		t.Fatalf("InUse = %d, want 2", p.InUse())
	}

	ctx2, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := p.Acquire(ctx2); err == nil {
		t.Fatal("expected acquire to fail when pool full")
	}

	r1.Release()
	r3, err := p.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r2.Release()
	r3.Release()
	if p.InUse() != 0 {
		t.Fatalf("InUse = %d after release, want 0", p.InUse())
	}
}

func TestPool_NeverExceedsMaxUnderConcurrency(t *testing.T) {
	t.Parallel()
	const max = 2
	p := ratelimit.NewPool(max)
	var peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := p.Acquire(context.Background())
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			cur := int32(p.InUse())
			for {
				old := peak.Load()
				if cur <= old || peak.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			r.Release()
		}()
	}
	wg.Wait()
	if peak.Load() > max {
		t.Fatalf("peak concurrent = %d, exceeds max %d", peak.Load(), max)
	}
	if p.InUse() != 0 {
		t.Fatalf("leaked slots: %d", p.InUse())
	}
}

func TestByteLimiter_CapsAverageRate(t *testing.T) {
	t.Parallel()
	// 8 KiB/s
	lim := ratelimit.NewByteLimiter(8 * 1024)
	buf := make([]byte, 4*1024)
	start := time.Now()
	var total int
	for total < 16*1024 {
		n, err := lim.WaitN(context.Background(), len(buf))
		if err != nil {
			t.Fatal(err)
		}
		total += n
	}
	elapsed := time.Since(start)
	// 16KiB at 8KiB/s ≈ 2s; allow scheduler slack
	if elapsed < 1400*time.Millisecond {
		t.Fatalf("elapsed %v too fast for 16KiB at 8KiB/s", elapsed)
	}
}
