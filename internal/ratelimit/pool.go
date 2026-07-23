package ratelimit

import (
	"context"
	"errors"
	"sync"
)

// ErrPoolClosed is returned when acquiring from a closed pool.
var ErrPoolClosed = errors.New("ratelimit: pool closed")

// Pool limits concurrent upstream acquisitions (e.g. IPTV "2 streams").
type Pool struct {
	sem    chan struct{}
	mu     sync.Mutex
	inUse  int
	closed bool
}

// NewPool creates a pool with max concurrent slots. max < 1 becomes 1.
func NewPool(max int) *Pool {
	if max < 1 {
		max = 1
	}
	return &Pool{sem: make(chan struct{}, max)}
}

// Lease is a held pool slot; call Release when done.
type Lease struct {
	p *Pool
}

// Acquire blocks until a slot is free or ctx is done.
func (p *Pool) Acquire(ctx context.Context) (*Lease, error) {
	select {
	case p.sem <- struct{}{}:
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			<-p.sem
			return nil, ErrPoolClosed
		}
		p.inUse++
		p.mu.Unlock()
		return &Lease{p: p}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Release returns the slot to the pool. Safe to call once.
func (l *Lease) Release() {
	if l == nil || l.p == nil {
		return
	}
	p := l.p
	l.p = nil
	p.mu.Lock()
	if p.inUse > 0 {
		p.inUse--
	}
	p.mu.Unlock()
	select {
	case <-p.sem:
	default:
	}
}

// InUse returns current held slots.
func (p *Pool) InUse() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inUse
}

// Max returns capacity.
func (p *Pool) Max() int {
	return cap(p.sem)
}
