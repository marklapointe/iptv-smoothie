package stream

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// parsedRange is a single byte range (inclusive start/end).
type parsedRange struct {
	start, end int64 // end inclusive; -1 end means until EOF at open time
}

// parseRangeHeader parses a simple bytes=START-END header (single range only).
func parseRangeHeader(h string, size int64) (parsedRange, bool) {
	h = strings.TrimSpace(h)
	if !strings.HasPrefix(h, "bytes=") {
		return parsedRange{}, false
	}
	spec := strings.TrimPrefix(h, "bytes=")
	if strings.Contains(spec, ",") {
		// multi-range not supported
		return parsedRange{}, false
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return parsedRange{}, false
	}
	var start, end int64
	var err error
	if parts[0] == "" {
		// suffix: bytes=-N
		n, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || n <= 0 || size <= 0 {
			return parsedRange{}, false
		}
		if n > size {
			n = size
		}
		start = size - n
		end = size - 1
		return parsedRange{start: start, end: end}, true
	}
	start, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 {
		return parsedRange{}, false
	}
	if parts[1] == "" {
		if size > 0 {
			end = size - 1
		} else {
			end = -1
		}
	} else {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return parsedRange{}, false
		}
	}
	if size > 0 {
		if start >= size {
			return parsedRange{}, false
		}
		if end < 0 || end >= size {
			end = size - 1
		}
	}
	return parsedRange{start: start, end: end}, true
}

func openFileRange(path string, rng parsedRange, size int64) (io.ReadCloser, int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, "", err
	}
	if rng.end < 0 {
		rng.end = size - 1
	}
	length := rng.end - rng.start + 1
	if length <= 0 {
		_ = f.Close()
		return nil, 0, "", fmt.Errorf("stream: invalid range length")
	}
	if _, err := f.Seek(rng.start, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, 0, "", err
	}
	cr := fmt.Sprintf("bytes %d-%d/%d", rng.start, rng.end, size)
	return &limitedReadCloser{r: io.LimitReader(f, length), c: f}, length, cr, nil
}

type limitedReadCloser struct {
	r io.Reader
	c io.Closer
}

func (l *limitedReadCloser) Read(p []byte) (int, error) { return l.r.Read(p) }
func (l *limitedReadCloser) Close() error               { return l.c.Close() }
