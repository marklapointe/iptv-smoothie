package cache

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ValidateOpts controls validation strictness.
type ValidateOpts struct {
	ExpectedSize int64 // 0 = skip size check
	MinSize      int64 // default 1
}

// ValidateFile ensures a completed download is safe to promote to the library.
func ValidateFile(path string, opts ValidateOpts) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("cache: not a regular file")
	}
	min := opts.MinSize
	if min <= 0 {
		min = 1
	}
	if st.Size() < min {
		return fmt.Errorf("cache: file too small (%d)", st.Size())
	}
	if opts.ExpectedSize > 0 && st.Size() != opts.ExpectedSize {
		return fmt.Errorf("cache: size mismatch got=%d want=%d", st.Size(), opts.ExpectedSize)
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	if n == 0 {
		return fmt.Errorf("cache: empty file")
	}
	// Reject HTML/JSON error pages saved as media
	trim := bytes.TrimSpace(head)
	low := bytes.ToLower(trim)
	if bytes.HasPrefix(low, []byte("<!doctype html")) || bytes.HasPrefix(low, []byte("<html")) ||
		bytes.HasPrefix(low, []byte("{")) && bytes.Contains(low, []byte("error")) {
		return fmt.Errorf("cache: content looks like an error page, not media")
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp4", ".m4v", ".mov":
		if !bytes.Contains(head, []byte("ftyp")) {
			return fmt.Errorf("cache: missing mp4 ftyp atom")
		}
	case ".mkv", ".webm":
		// EBML header 0x1A45DFA3
		if n >= 4 && !(head[0] == 0x1A && head[1] == 0x45 && head[2] == 0xDF && head[3] == 0xA3) {
			return fmt.Errorf("cache: missing matroska EBML header")
		}
	case ".ts", ".mts":
		// MPEG-TS sync 0x47 every 188 bytes — check first byte
		if head[0] != 0x47 {
			return fmt.Errorf("cache: missing mpeg-ts sync")
		}
	case ".bin", "":
		// allow raw for tests
	default:
		// unknown ext: magic-only soft pass if not html
	}
	return nil
}
