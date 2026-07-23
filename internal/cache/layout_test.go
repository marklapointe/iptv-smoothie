package cache_test

import (
	"path/filepath"
	"testing"

	"github.com/mlapointe/smoothie/internal/cache"
)

func TestLibraryPath_Movie(t *testing.T) {
	t.Parallel()
	p, err := cache.LibraryPath("/media", cache.MediaMeta{
		Kind:  cache.KindMovie,
		Title: "The Matrix",
		Year:  1999,
		Ext:   ".mkv",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/media", "movies", "The Matrix (1999)", "The Matrix (1999).mkv")
	if p != want {
		t.Fatalf("got %q want %q", p, want)
	}
}

func TestLibraryPath_TVSeason(t *testing.T) {
	t.Parallel()
	p, err := cache.LibraryPath("/media", cache.MediaMeta{
		Kind:    cache.KindEpisode,
		Show:    "Example Show",
		Season:  1,
		Episode: 5,
		Title:   "Pilot",
		Ext:     ".mp4",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/media", "tv", "Example Show", "Season 01", "Example Show - S01E05 - Pilot.mp4")
	if p != want {
		t.Fatalf("got %q want %q", p, want)
	}
}

func TestLibraryPath_SanitizesTraversal(t *testing.T) {
	t.Parallel()
	p, err := cache.LibraryPath("/media", cache.MediaMeta{
		Kind:  cache.KindMovie,
		Title: "../../etc/passwd",
		Year:  2020,
		Ext:   ".mp4",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Must stay under /media/movies
	if filepath.Dir(filepath.Dir(p)) != filepath.Join("/media", "movies") {
		t.Fatalf("unsafe path: %s", p)
	}
	if filepath.Base(filepath.Dir(p)) == "etc" {
		t.Fatalf("traversal: %s", p)
	}
}
