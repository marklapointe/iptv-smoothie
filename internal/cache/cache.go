package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Object states.
const (
	StateFilling    = "filling"
	StateValidated  = "validated"
	StateCorrupt    = "corrupt"
	StateMissing    = "missing"
)

// Config for disk cache + purgatory + library promote.
type Config struct {
	Root         string
	LibraryRoot  string
	MaxBytes     int64
	TTL          time.Duration
	MinFreeBytes int64
}

// Object is a cache/purgatory entry.
type Object struct {
	Key          string
	Path         string
	State        string
	Size         int64
	ExpectedSize int64
	Ext          string
	LastAccess   time.Time
	Pin          bool
}

// Cache manages progressive fills under cache/ and purgatory/.
type Cache struct {
	cfg   Config
	mu    sync.Mutex
	objs  map[string]*Object
	fills map[string]*fillState
}

type fillState struct {
	done chan struct{}
	err  error
}

// New creates cache dirs and an in-memory index (file-backed objects).
func New(cfg Config) (*Cache, error) {
	if cfg.Root == "" {
		return nil, errors.New("cache: empty root")
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = 50 << 30
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 7 * 24 * time.Hour
	}
	for _, sub := range []string{"cache", "purgatory"} {
		if err := os.MkdirAll(filepath.Join(cfg.Root, sub), 0o750); err != nil {
			return nil, err
		}
	}
	if cfg.LibraryRoot != "" {
		for _, sub := range []string{"movies", "tv"} {
			_ = os.MkdirAll(filepath.Join(cfg.LibraryRoot, sub), 0o750)
		}
	}
	return &Cache{
		cfg:   cfg,
		objs:  make(map[string]*Object),
		fills: make(map[string]*fillState),
	}, nil
}

// Get returns object metadata if known.
func (c *Cache) Get(key string) (*Object, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	o, ok := c.objs[key]
	if !ok {
		return nil, errors.New("cache: not found")
	}
	cp := *o
	return &cp, nil
}

// OpenOrFill returns a reader for key. If missing, src must be non-nil to fill.
// Reader is progressive (reads from file as it grows / complete file on hit).
func (c *Cache) OpenOrFill(ctx context.Context, key string, src io.ReadCloser, expected int64, ext string) (*Object, io.ReadCloser, error) {
	if key == "" {
		return nil, nil, errors.New("cache: empty key")
	}
	if ext == "" {
		ext = ".bin"
	}
	if !hasDot(ext) {
		ext = "." + ext
	}

	c.mu.Lock()
	if o, ok := c.objs[key]; ok && o.State == StateValidated {
		o.LastAccess = time.Now()
		path := o.Path
		cp := *o
		c.mu.Unlock()
		f, err := os.Open(path)
		if err != nil {
			return nil, nil, err
		}
		return &cp, f, nil
	}
	// already filling?
	if fs, ok := c.fills[key]; ok {
		c.mu.Unlock()
		select {
		case <-fs.done:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
		return c.OpenOrFill(ctx, key, nil, expected, ext)
	}

	if src == nil {
		c.mu.Unlock()
		return nil, nil, errors.New("cache: miss and no source")
	}

	path := filepath.Join(c.cfg.Root, "purgatory", safeKey(key)+ext)
	o := &Object{
		Key:          key,
		Path:         path,
		State:        StateFilling,
		ExpectedSize: expected,
		Ext:          ext,
		LastAccess:   time.Now(),
	}
	c.objs[key] = o
	fs := &fillState{done: make(chan struct{})}
	c.fills[key] = fs
	c.mu.Unlock()

	// create file and start fill in background; return progressive reader
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o640)
	if err != nil {
		c.failFill(key, fs, err)
		_ = src.Close()
		return nil, nil, err
	}

	go c.runFill(key, f, src, expected, fs)

	// progressive reader on same path
	pr, err := openProgressive(path, fs.done)
	if err != nil {
		return nil, nil, err
	}
	cp := *o
	return &cp, pr, nil
}

func (c *Cache) runFill(key string, f *os.File, src io.ReadCloser, expected int64, fs *fillState) {
	defer func() {
		_ = src.Close()
		c.mu.Lock()
		delete(c.fills, key)
		close(fs.done)
		c.mu.Unlock()
	}()

	n, copyErr := io.Copy(f, src)
	name := f.Name()
	_ = f.Sync()
	_ = f.Close()

	c.mu.Lock()
	o := c.objs[key]
	if o == nil {
		c.mu.Unlock()
		return
	}
	o.Size = n
	if copyErr != nil {
		o.State = StateCorrupt
		fs.err = copyErr
		c.mu.Unlock()
		return
	}
	opts := ValidateOpts{ExpectedSize: expected}
	if expected <= 0 {
		opts.ExpectedSize = 0
	}
	// Unlock during ValidateFile (disk I/O)
	c.mu.Unlock()
	valErr := ValidateFile(name, opts)
	c.mu.Lock()
	o = c.objs[key]
	if o == nil {
		c.mu.Unlock()
		return
	}
	if valErr != nil {
		o.State = StateCorrupt
		fs.err = valErr
		c.mu.Unlock()
		return
	}
	// move purgatory -> cache on success
	dest := filepath.Join(c.cfg.Root, "cache", filepath.Base(name))
	c.mu.Unlock()
	if err := os.Rename(name, dest); err == nil {
		c.mu.Lock()
		if o = c.objs[key]; o != nil {
			o.Path = dest
			o.State = StateValidated
			o.LastAccess = time.Now()
		}
		c.mu.Unlock()
		return
	}
	c.mu.Lock()
	if o = c.objs[key]; o != nil {
		o.State = StateValidated
		o.LastAccess = time.Now()
	}
	c.mu.Unlock()
}

func (c *Cache) failFill(key string, fs *fillState, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if o := c.objs[key]; o != nil {
		o.State = StateCorrupt
	}
	fs.err = err
	delete(c.fills, key)
	close(fs.done)
}

// Promote copies a validated object into the library tree (movies vs tv/season).
func (c *Cache) Promote(key string, meta MediaMeta) (string, error) {
	c.mu.Lock()
	o, ok := c.objs[key]
	if !ok || o.State != StateValidated {
		c.mu.Unlock()
		return "", fmt.Errorf("cache: not validated")
	}
	srcPath := o.Path
	c.mu.Unlock()

	if c.cfg.LibraryRoot == "" {
		return "", errors.New("cache: library root not configured")
	}
	if meta.Ext == "" {
		meta.Ext = o.Ext
	}
	dest, err := LibraryPath(c.cfg.LibraryRoot, meta)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return "", err
	}
	// copy (keep cache for TTL)
	in, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dest)
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	// re-validate destination
	if err := ValidateFile(dest, ValidateOpts{}); err != nil {
		_ = os.Remove(dest)
		return "", err
	}
	return dest, nil
}

// Reap removes expired / over-budget unpinned objects.
func (c *Cache) Reap(ctx context.Context) (int, error) {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	removed := 0

	// TTL first
	for k, o := range c.objs {
		if o.Pin || o.State == StateFilling {
			continue
		}
		if now.Sub(o.LastAccess) > c.cfg.TTL {
			_ = os.Remove(o.Path)
			delete(c.objs, k)
			removed++
		}
	}
	// size budget
	for {
		var total int64
		for _, o := range c.objs {
			total += o.Size
		}
		if total <= c.cfg.MaxBytes {
			break
		}
		// LRU victim
		var victim string
		var oldest time.Time
		for k, o := range c.objs {
			if o.Pin || o.State == StateFilling {
				continue
			}
			if victim == "" || o.LastAccess.Before(oldest) {
				victim = k
				oldest = o.LastAccess
			}
		}
		if victim == "" {
			break
		}
		_ = os.Remove(c.objs[victim].Path)
		delete(c.objs, victim)
		removed++
	}
	return removed, nil
}

func safeKey(key string) string {
	return sanitizeName(key)
}

func hasDot(ext string) bool {
	return len(ext) > 0 && ext[0] == '.'
}

// progressiveReader reads a growing file until done is closed and EOF reached.
type progressiveReader struct {
	path string
	done <-chan struct{}
	off  int64
}

func openProgressive(path string, done <-chan struct{}) (io.ReadCloser, error) {
	// ensure file exists
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(path); err == nil {
			return &progressiveReader{path: path, done: done}, nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil, fmt.Errorf("cache: progressive open timeout for %s", path)
}

func (p *progressiveReader) Read(b []byte) (int, error) {
	for {
		f, err := os.Open(p.path)
		if err != nil {
			select {
			case <-p.done:
				return 0, io.EOF
			case <-time.After(10 * time.Millisecond):
				continue
			}
		}
		if _, err = f.Seek(p.off, io.SeekStart); err != nil {
			_ = f.Close()
			return 0, err
		}
		n, err := f.Read(b)
		_ = f.Close()
		if n > 0 {
			p.off += int64(n)
			return n, nil
		}
		if err != nil && err != io.EOF {
			return 0, err
		}
		// no new bytes
		select {
		case <-p.done:
			// one last attempt after fill finished (rename may have moved file)
			if p.off > 0 {
				// try cache path sibling if purgatory renamed
			}
			f2, err2 := os.Open(p.path)
			if err2 != nil {
				// check cache dir same basename
				alt := filepath.Join(filepath.Dir(filepath.Dir(p.path)), "cache", filepath.Base(p.path))
				f2, err2 = os.Open(alt)
				if err2 != nil {
					return 0, io.EOF
				}
				p.path = alt
			}
			if _, err2 = f2.Seek(p.off, io.SeekStart); err2 != nil {
				_ = f2.Close()
				return 0, io.EOF
			}
			n, err = f2.Read(b)
			_ = f2.Close()
			if n > 0 {
				p.off += int64(n)
				return n, nil
			}
			return 0, io.EOF
		case <-time.After(15 * time.Millisecond):
		}
	}
}

func (p *progressiveReader) Close() error {
	return nil
}
