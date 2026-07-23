package cache_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mlapointe/smoothie/internal/cache"
)

func TestValidate_RejectsHTMLAsMP4(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.mp4")
	if err := os.WriteFile(p, []byte("<html>error</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := cache.ValidateFile(p, cache.ValidateOpts{ExpectedSize: int64(len("<html>error</html>"))})
	if err == nil {
		t.Fatal("expected reject html masquerading as media")
	}
}

func TestValidate_AcceptsMP4Magic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "ok.mp4")
	// minimal ftyp box-ish header
	data := []byte{
		0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p',
		'i', 's', 'o', 'm', 0x00, 0x00, 0x02, 0x00,
		'i', 's', 'o', 'm', 'i', 's', 'o', '2',
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cache.ValidateFile(p, cache.ValidateOpts{ExpectedSize: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
}

func TestValidate_SizeMismatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "trunc.mp4")
	data := []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	err := cache.ValidateFile(p, cache.ValidateOpts{ExpectedSize: 9999})
	if err == nil {
		t.Fatal("expected size mismatch")
	}
}
