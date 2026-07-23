package stream

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestParseRangeHeader(t *testing.T) {
	t.Parallel()
	r, ok := parseRangeHeader("bytes=0-99", 1000)
	if !ok || r.start != 0 || r.end != 99 {
		t.Fatalf("%+v ok=%v", r, ok)
	}
	r, ok = parseRangeHeader("bytes=100-", 1000)
	if !ok || r.start != 100 || r.end != 999 {
		t.Fatalf("%+v", r)
	}
	r, ok = parseRangeHeader("bytes=-50", 1000)
	if !ok || r.start != 950 || r.end != 999 {
		t.Fatalf("%+v", r)
	}
	if _, ok := parseRangeHeader("bytes=0-10,11-20", 100); ok {
		t.Fatal("multi-range should fail")
	}
}

func TestOpenFileRange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	data := []byte("0123456789ABCDEF")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	rc, length, cr, err := openFileRange(p, parsedRange{start: 4, end: 7}, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if length != 4 || cr != "bytes 4-7/16" {
		t.Fatalf("length=%d cr=%s", length, cr)
	}
	b, _ := io.ReadAll(rc)
	if string(b) != "4567" {
		t.Fatalf("got %q", b)
	}
}
