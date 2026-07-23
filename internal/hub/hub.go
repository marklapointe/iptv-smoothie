package hub

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

// OpenFunc opens a single upstream reader for a channel key.
type OpenFunc func() (io.ReadCloser, error)

// Options configures hub behavior.
type Options struct {
	IdleGrace time.Duration
	ClientBuf int
	ChunkSize int
}

// Hub fans one upstream out to many subscribers per key.
type Hub struct {
	opts     Options
	mu       sync.Mutex
	sess     map[string]*session
	creating map[string]chan struct{} // singleflight per key
}

type session struct {
	key      string
	upstream io.ReadCloser
	subs     map[int]chan []byte
	nextID   int
	done     chan struct{}
	mu       sync.Mutex
	started  bool
}

// New creates a Hub.
func New(opts Options) *Hub {
	if opts.IdleGrace <= 0 {
		opts.IdleGrace = 3 * time.Second
	}
	if opts.ClientBuf <= 0 {
		opts.ClientBuf = 32
	}
	if opts.ChunkSize <= 0 {
		opts.ChunkSize = 32 * 1024
	}
	return &Hub{
		opts:     opts,
		sess:     make(map[string]*session),
		creating: make(map[string]chan struct{}),
	}
}

// Subscriber is a readable fan-out stream; Close unsubscribes.
type Subscriber struct {
	ch   <-chan []byte
	s    *session
	id   int
	buf  []byte
	once sync.Once
}

// Subscribe joins (or starts) the fan-out session for key.
func (h *Hub) Subscribe(ctx context.Context, key string, open OpenFunc) (*Subscriber, error) {
	if key == "" {
		return nil, errors.New("hub: empty key")
	}
	if open == nil {
		return nil, errors.New("hub: nil open func")
	}

	for {
		h.mu.Lock()
		if s, ok := h.liveSession(key); ok {
			sub := h.addSubLocked(s)
			h.mu.Unlock()
			h.watch(ctx, sub, s)
			return sub, nil
		}
		if wait, ok := h.creating[key]; ok {
			h.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		// This goroutine creates the session.
		done := make(chan struct{})
		h.creating[key] = done
		h.mu.Unlock()

		up, err := open()
		if err != nil {
			h.mu.Lock()
			delete(h.creating, key)
			close(done)
			h.mu.Unlock()
			return nil, err
		}

		h.mu.Lock()
		s := &session{
			key:      key,
			upstream: up,
			subs:     make(map[int]chan []byte),
			done:     make(chan struct{}),
			started:  true,
		}
		h.sess[key] = s
		sub := h.addSubLocked(s)
		delete(h.creating, key)
		close(done)
		h.mu.Unlock()

		go h.pump(s)
		h.watch(ctx, sub, s)
		return sub, nil
	}
}

func (h *Hub) liveSession(key string) (*session, bool) {
	s, ok := h.sess[key]
	if !ok {
		return nil, false
	}
	select {
	case <-s.done:
		delete(h.sess, key)
		return nil, false
	default:
		return s, true
	}
}

func (h *Hub) addSubLocked(s *session) *Subscriber {
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	ch := make(chan []byte, h.opts.ClientBuf)
	s.subs[id] = ch
	s.mu.Unlock()
	return &Subscriber{ch: ch, s: s, id: id}
}

func (h *Hub) watch(ctx context.Context, sub *Subscriber, s *session) {
	go func() {
		select {
		case <-ctx.Done():
			_ = sub.Close()
		case <-s.done:
		}
	}()
}

func (h *Hub) pump(s *session) {
	defer func() {
		s.mu.Lock()
		for id, ch := range s.subs {
			close(ch)
			delete(s.subs, id)
		}
		s.mu.Unlock()
		_ = s.upstream.Close()
		close(s.done)
		time.Sleep(h.opts.IdleGrace)
		h.mu.Lock()
		if cur, ok := h.sess[s.key]; ok && cur == s {
			delete(h.sess, s.key)
		}
		h.mu.Unlock()
	}()

	buf := make([]byte, h.opts.ChunkSize)
	for {
		n, err := s.upstream.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.mu.Lock()
			for _, ch := range s.subs {
				select {
				case ch <- chunk:
				default:
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// Read implements io.Reader for a subscriber.
func (sub *Subscriber) Read(p []byte) (int, error) {
	if len(sub.buf) > 0 {
		n := copy(p, sub.buf)
		sub.buf = sub.buf[n:]
		return n, nil
	}
	chunk, ok := <-sub.ch
	if !ok {
		return 0, io.EOF
	}
	n := copy(p, chunk)
	if n < len(chunk) {
		sub.buf = chunk[n:]
	}
	return n, nil
}

// Close unsubscribes.
func (sub *Subscriber) Close() error {
	sub.once.Do(func() {
		s := sub.s
		s.mu.Lock()
		if ch, ok := s.subs[sub.id]; ok {
			delete(s.subs, sub.id)
			select {
			case <-s.done:
			default:
				close(ch)
			}
		}
		s.mu.Unlock()
	})
	return nil
}

// ActiveSessions returns number of live fan-out keys.
func (h *Hub) ActiveSessions() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sess)
}
