package cache

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// Media kinds for library layout.
const (
	KindMovie   = "movie"
	KindEpisode = "episode"
)

// MediaMeta describes naming for promote into Emby-style trees.
type MediaMeta struct {
	Kind    string
	Title   string
	Show    string
	Season  int
	Episode int
	Year    int
	Ext     string
}

// LibraryPath builds movies/ vs tv/Show/Season NN/ paths under libraryRoot.
func LibraryPath(libraryRoot string, m MediaMeta) (string, error) {
	if libraryRoot == "" {
		return "", fmt.Errorf("cache: empty library root")
	}
	ext := m.Ext
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	if ext == "" {
		ext = ".mp4"
	}
	switch m.Kind {
	case KindMovie:
		title := sanitizeName(m.Title)
		if title == "" {
			return "", fmt.Errorf("cache: movie title required")
		}
		folder := title
		if m.Year > 0 {
			folder = fmt.Sprintf("%s (%d)", title, m.Year)
		}
		file := folder + ext
		return filepath.Join(libraryRoot, "movies", folder, file), nil
	case KindEpisode:
		show := sanitizeName(m.Show)
		if show == "" {
			show = sanitizeName(m.Title)
		}
		if show == "" {
			return "", fmt.Errorf("cache: show name required")
		}
		if m.Season < 0 {
			m.Season = 0
		}
		if m.Episode < 0 {
			m.Episode = 0
		}
		epTitle := sanitizeName(m.Title)
		if epTitle == "" {
			epTitle = "Episode"
		}
		seasonDir := fmt.Sprintf("Season %02d", m.Season)
		file := fmt.Sprintf("%s - S%02dE%02d - %s%s", show, m.Season, m.Episode, epTitle, ext)
		return filepath.Join(libraryRoot, "tv", show, seasonDir, file), nil
	default:
		return "", fmt.Errorf("cache: unknown media kind %q", m.Kind)
	}
}

func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// strip path separators and nulls
	s = strings.ReplaceAll(s, string(filepath.Separator), "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, "..", "_")
	var b strings.Builder
	for _, r := range s {
		if r == 0 || !unicode.IsPrint(r) {
			continue
		}
		switch r {
		case '<', '>', ':', '"', '|', '?', '*':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	out = strings.Trim(out, ".")
	if out == "" || out == "." || out == ".." {
		return "unknown"
	}
	return out
}
