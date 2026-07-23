package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/mlapointe/smoothie/internal/cache"
	"github.com/mlapointe/smoothie/internal/emby"
	"github.com/mlapointe/smoothie/internal/store"
)

func (s *Server) handleCacheStats(w http.ResponseWriter, r *http.Request) {
	if s.Cache == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": true,
		// lightweight: object count via promote-ready not tracked globally yet
		"note": "cache enabled; objects tracked in-process",
	})
}

type promoteReq struct {
	Kind    string `json:"kind"` // movie | episode
	Title   string `json:"title"`
	Show    string `json:"show"`
	Season  int    `json:"season"`
	Episode int    `json:"episode"`
	Year    int    `json:"year"`
	Ext     string `json:"ext"`
}

func (s *Server) handlePromote(w http.ResponseWriter, r *http.Request) {
	if s.Cache == nil {
		writeErr(w, http.StatusServiceUnavailable, "cache not configured")
		return
	}
	id := r.PathValue("id")
	ch, err := s.DB.GetChannel(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "channel not found")
		return
	}
	var req promoteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Title == "" {
		req.Title = ch.Name
	}
	if req.Kind == "" {
		if strings.Contains(strings.ToLower(ch.GroupName), "series") ||
			strings.Contains(strings.ToLower(ch.StreamURL), "/series/") {
			req.Kind = cache.KindEpisode
		} else {
			req.Kind = cache.KindMovie
		}
	}
	key := ch.SourceID + ":" + ch.ID
	path, err := s.Cache.Promote(key, cache.MediaMeta{
		Kind:    req.Kind,
		Title:   req.Title,
		Show:    req.Show,
		Season:  req.Season,
		Episode: req.Episode,
		Year:    req.Year,
		Ext:     req.Ext,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	embyRefreshed := false
	if url, _ := s.DB.GetSetting("emby.url"); url != "" {
		key, _ := s.DB.GetSetting("emby.api_key")
		if key != "" {
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()
			cli := emby.New(url, key)
			if err := cli.RefreshLibrary(ctx); err == nil {
				embyRefreshed = true
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path": path, "channel_id": id, "emby_refreshed": embyRefreshed,
	})
}

type embyConfigReq struct {
	URL    string `json:"url"`
	APIKey string `json:"api_key"`
}

func (s *Server) handleEmbyConfig(w http.ResponseWriter, r *http.Request) {
	var req embyConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.URL == "" || req.APIKey == "" {
		writeErr(w, http.StatusBadRequest, "url and api_key required")
		return
	}
	_ = s.DB.SetSetting("emby.url", strings.TrimRight(req.URL, "/"))
	_ = s.DB.SetSetting("emby.api_key", req.APIKey)
	cli := emby.New(req.URL, req.APIKey)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := cli.Ping(ctx); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"saved": true, "reachable": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "reachable": true})
}

func (s *Server) handleEmbyStatus(w http.ResponseWriter, r *http.Request) {
	url, err := s.DB.GetSetting("emby.url")
	if err != nil || url == "" {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": true,
		"url":        url,
		// never echo api key
	})
}

type libraryRootReq struct {
	Kind   string `json:"kind"` // movie | tv
	FSPath string `json:"fs_path"`
}

func (s *Server) handleSetLibraryRoot(w http.ResponseWriter, r *http.Request) {
	var req libraryRootReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Kind != "movie" && req.Kind != "tv" {
		writeErr(w, http.StatusBadRequest, "kind must be movie or tv")
		return
	}
	if req.FSPath == "" {
		writeErr(w, http.StatusBadRequest, "fs_path required")
		return
	}
	req.FSPath = filepath.Clean(req.FSPath)
	// upsert by kind
	existing, _ := s.DB.ListLibraryRoots()
	for _, e := range existing {
		if e.Kind == req.Kind {
			e.FSPath = req.FSPath
			if err := s.DB.SaveLibraryRoot(&e); err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, e)
			return
		}
	}
	lr := store.LibraryRoot{Kind: req.Kind, FSPath: req.FSPath}
	if err := s.DB.CreateLibraryRoot(&lr); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, lr)
}

func (s *Server) handleListLibraryRoots(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.ListLibraryRoots()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}
