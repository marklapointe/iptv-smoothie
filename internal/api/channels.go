package api

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mlapointe/smoothie/internal/playlist"
	"github.com/mlapointe/smoothie/internal/source"
	"github.com/mlapointe/smoothie/internal/store"
)

func (s *Server) handleRefreshSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ref := source.NewRefresher(s.DB)
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	res, err := ref.RefreshSource(ctx, id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	list, err := s.DB.ListChannels(q.Get("source_id"), q.Get("kind"), q.Get("q"), limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	total, _ := s.DB.CountChannels(q.Get("source_id"))
	// Never expose upstream stream URLs (often embed portal credentials) to the UI.
	items := make([]map[string]any, 0, len(list))
	for _, c := range list {
		items = append(items, map[string]any{
			"id":         c.ID,
			"source_id":  c.SourceID,
			"remote_key": c.RemoteKey,
			"name":       c.Name,
			"group_name": c.GroupName,
			"kind":       c.Kind,
			"logo":       c.Logo,
			"enabled":    c.Enabled,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (s *Server) handlePlaylist(w http.ResponseWriter, r *http.Request) {
	// Public LAN playlist (token query optional later)
	base := s.BaseURL
	if base == "" {
		scheme := "http"
		base = scheme + "://" + r.Host
	}
	// Stream rewrite — cap for huge catalogs unless filtered
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 5000
	}
	chs, err := s.DB.ListChannels(q.Get("source_id"), q.Get("kind"), q.Get("q"), limit, 0)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]playlist.RewriteItem, 0, len(chs))
	for _, c := range chs {
		items = append(items, playlist.RewriteItem{
			ChannelID: c.ID,
			Entry: playlist.Entry{
				Name:    c.Name,
				Group:   c.GroupName,
				Logo:    c.Logo,
				Kind:    playlist.Kind(c.Kind),
				TvgName: c.Name,
			},
		})
	}
	w.Header().Set("Content-Type", "application/x-mpegURL")
	w.Header().Set("Content-Disposition", "inline; filename=smoothie.m3u")
	_ = playlist.Write(w, base, items)
}

func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ch, err := s.DB.GetChannel(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "channel not found")
		return
	}
	if ch.StreamURL == "" {
		writeErr(w, http.StatusBadGateway, "no stream url")
		return
	}
	if s.Streams == nil {
		writeErr(w, http.StatusServiceUnavailable, "stream manager not configured")
		return
	}

	// Live: fan-out hub (1 upstream, N LAN clients)
	if ch.Kind != store.ChannelKindVOD && !looksLikeVODURL(ch.StreamURL) {
		body, err := s.Streams.OpenLive(r.Context(), ch)
		if err != nil {
			writeErr(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		defer body.Close()
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, body)
		return
	}

	// VOD: progressive cache fill (or direct Range proxy)
	res, err := s.Streams.OpenVOD(r.Context(), ch, r.Header.Get("Range"))
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	defer res.Body.Close()

	ct := res.ContentType
	if ct == "" {
		ct = "video/mp4"
	}
	w.Header().Set("Content-Type", ct)
	if res.CacheHit {
		w.Header().Set("X-Smoothie-Cache", "HIT")
	} else {
		w.Header().Set("X-Smoothie-Cache", "MISS")
	}
	if res.ContentLength > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(res.ContentLength, 10))
	}
	w.Header().Set("Accept-Ranges", "bytes")
	status := res.StatusCode
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = io.Copy(w, res.Body)
}

func looksLikeVODURL(u string) bool {
	l := strings.ToLower(u)
	if strings.Contains(l, "/movie/") || strings.Contains(l, "/series/") || strings.Contains(l, "/vod/") {
		return true
	}
	for _, ext := range []string{".mp4", ".mkv", ".avi", ".m4v", ".mov"} {
		if strings.Contains(l, ext) {
			return true
		}
	}
	return false
}
