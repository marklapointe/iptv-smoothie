package ratelimit

import (
	"context"
	"time"
)

// ByteLimiter is a simple token-bucket style average rate limiter for bytes/sec.
type ByteLimiter struct {
	bps    int64
	tokens float64
	last   time.Time
}

// NewByteLimiter caps average throughput at bps bytes per second. bps < 1 means unlimited.
// Starts with zero tokens so the first burst still respects sustained rate.
func NewByteLimiter(bps int64) *ByteLimiter {
	return &ByteLimiter{bps: bps, tokens: 0, last: time.Now()}
}

// WaitN waits until n bytes (or a partial chunk) may be transferred under the rate cap.
// Returns the allowed byte count (≤ n).
func (b *ByteLimiter) WaitN(ctx context.Context, n int) (int, error) {
	if n <= 0 {
		return 0, nil
	}
	if b.bps < 1 {
		return n, nil
	}

	// Cap single wait to 64KiB slices for responsiveness
	want := n
	if want > 64*1024 {
		want = 64 * 1024
	}

	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		now := time.Now()
		elapsed := now.Sub(b.last).Seconds()
		b.last = now
		b.tokens += elapsed * float64(b.bps)
		if b.tokens > float64(b.bps) {
			b.tokens = float64(b.bps)
		}
		if b.tokens >= 1 {
			allow := want
			if float64(allow) > b.tokens {
				allow = int(b.tokens)
			}
			if allow < 1 {
				allow = 1
			}
			b.tokens -= float64(allow)
			return allow, nil
		}
		// Need more tokens
		need := 1.0 - b.tokens
		wait := time.Duration(need / float64(b.bps) * float64(time.Second))
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return 0, ctx.Err()
		case <-t.C:
		}
	}
}
