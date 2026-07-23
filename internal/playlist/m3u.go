package playlist

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// Kind classifies an M3U entry.
type Kind string

const (
	KindLive Kind = "live"
	KindVOD  Kind = "vod"
)

// Entry is one parsed M3U item.
type Entry struct {
	Name      string
	TvgID     string
	TvgName   string
	Logo      string
	Group     string
	URL       string
	Kind      Kind
	RemoteKey string
	Attrs     map[string]string
}

// Handler receives each entry during streaming parse.
type Handler func(Entry) error

var attrRe = regexp.MustCompile(`([a-zA-Z0-9-]+)="([^"]*)"`)

// Parse streams an M3U playlist and invokes h for each entry (EXTINF + URL).
func Parse(r io.Reader, h Handler) error {
	if h == nil {
		return fmt.Errorf("playlist: nil handler")
	}
	sc := bufio.NewScanner(r)
	// Large lines exist in some catalogs
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	var pending *Entry
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#EXTM3U") {
			continue
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			e := parseExtinf(line)
			pending = &e
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		// URL line
		if pending == nil {
			continue
		}
		pending.URL = line
		pending.Kind = ClassifyKind(pending.Group, pending.URL)
		pending.RemoteKey = RemoteKey(pending.URL, pending.Name, pending.Group)
		if err := h(*pending); err != nil {
			return err
		}
		pending = nil
	}
	return sc.Err()
}

func parseExtinf(line string) Entry {
	// #EXTINF:-1 attrs...,Display Name
	e := Entry{Attrs: map[string]string{}}
	rest := strings.TrimPrefix(line, "#EXTINF:")
	// duration then optional attrs then comma name
	comma := strings.LastIndex(rest, ",")
	attrPart := rest
	if comma >= 0 {
		e.Name = strings.TrimSpace(rest[comma+1:])
		attrPart = rest[:comma]
	}
	// skip duration token
	if i := strings.IndexAny(attrPart, " \t"); i >= 0 {
		attrPart = attrPart[i+1:]
	}
	for _, m := range attrRe.FindAllStringSubmatch(attrPart, -1) {
		k, v := strings.ToLower(m[1]), m[2]
		e.Attrs[k] = v
		switch k {
		case "tvg-id":
			e.TvgID = v
		case "tvg-name":
			e.TvgName = v
		case "tvg-logo":
			e.Logo = v
		case "group-title":
			e.Group = v
		}
	}
	if e.Name == "" && e.TvgName != "" {
		e.Name = e.TvgName
	}
	return e
}

// ClassifyKind decides live vs vod from group title and stream URL.
func ClassifyKind(group, streamURL string) Kind {
	g := strings.ToLower(group)
	u := strings.ToLower(streamURL)

	// Explicit path markers (Xtream Codes style)
	if strings.Contains(u, "/movie/") || strings.Contains(u, "/series/") || strings.Contains(u, "/vod/") {
		return KindVOD
	}
	// File extensions strongly imply VOD
	ext := strings.ToLower(path.Ext(stripQuery(streamURL)))
	switch ext {
	case ".mp4", ".mkv", ".avi", ".m4v", ".mov", ".mpg", ".mpeg", ".wmv", ".flv":
		return KindVOD
	}

	// Group heuristics
	vodHints := []string{"movie", "movies", "series", "vod", "film", "anime", "netflix", "disney", "hulu", "hbo", "documentary", "documentaries", "kids", "cartoon", "reality", "comedy series", "classic series"}
	for _, h := range vodHints {
		if strings.Contains(g, h) {
			// "24/7" movie channels are often live loops — treat 24/7 as live if no file ext
			if strings.Contains(g, "24/7") || strings.Contains(g, "24-7") {
				return KindLive
			}
			return KindVOD
		}
	}
	if strings.Contains(g, "live") || strings.Contains(u, "/live/") {
		return KindLive
	}
	return KindLive
}

func stripQuery(raw string) string {
	if i := strings.Index(raw, "?"); i >= 0 {
		return raw[:i]
	}
	return raw
}

// RemoteKey builds a stable key for a stream URL (path-focused; ignores host churn when possible).
func RemoteKey(streamURL, name, group string) string {
	u, err := url.Parse(streamURL)
	if err != nil || u.Path == "" {
		sum := sha1.Sum([]byte(streamURL + "|" + name + "|" + group))
		return hex.EncodeToString(sum[:12])
	}
	// Prefer last path segment(s) which are often the stream id / filename
	p := strings.Trim(u.Path, "/")
	parts := strings.Split(p, "/")
	if len(parts) >= 1 {
		tail := parts[len(parts)-1]
		if len(parts) >= 3 {
			// user/pass/id or movie/user/pass/file
			tail = strings.Join(parts[len(parts)-3:], "/")
		}
		sum := sha1.Sum([]byte(tail))
		return hex.EncodeToString(sum[:12])
	}
	sum := sha1.Sum([]byte(streamURL))
	return hex.EncodeToString(sum[:12])
}

// RewriteItem is a channel bound for local playlist export.
type RewriteItem struct {
	ChannelID string
	Entry     Entry
}

// Write emits an M3U with local /play/{id} URLs.
func Write(w io.Writer, baseURL string, items []RewriteItem) error {
	baseURL = strings.TrimRight(baseURL, "/")
	if _, err := io.WriteString(w, "#EXTM3U\n"); err != nil {
		return err
	}
	for _, it := range items {
		logo := it.Entry.Logo
		line := fmt.Sprintf(`#EXTINF:-1 tvg-id="%s" tvg-name="%s" tvg-logo="%s" group-title="%s",%s`+"\n",
			escapeAttr(it.Entry.TvgID),
			escapeAttr(firstNonEmpty(it.Entry.TvgName, it.Entry.Name)),
			escapeAttr(logo),
			escapeAttr(it.Entry.Group),
			it.Entry.Name,
		)
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
		play := fmt.Sprintf("%s/play/%s\n", baseURL, it.ChannelID)
		if _, err := io.WriteString(w, play); err != nil {
			return err
		}
	}
	return nil
}

func escapeAttr(s string) string {
	return strings.ReplaceAll(s, `"`, `'`)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
