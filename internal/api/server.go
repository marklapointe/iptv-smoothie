package api

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/mlapointe/smoothie/internal/cache"
	"github.com/mlapointe/smoothie/internal/store"
	"github.com/mlapointe/smoothie/internal/stream"
)

// Server is the HTTP API.
type Server struct {
	DB      *store.DB
	Streams *stream.Manager
	Cache   *cache.Cache
	BaseURL string // public base for rewritten playlists, e.g. http://127.0.0.1:8787

	mu       sync.Mutex
	sessions map[string]session
}

// ServerOptions optional deps for New.
type ServerOptions struct {
	Cache *cache.Cache
}

type session struct {
	Username  string
	ExpiresAt time.Time
}

// New constructs an API server.
func New(db *store.DB, opts ...ServerOptions) *Server {
	var c *cache.Cache
	if len(opts) > 0 {
		c = opts[0].Cache
	}
	return &Server{
		DB:       db,
		Cache:    c,
		Streams:  stream.New(db, stream.Options{Cache: c}),
		sessions: make(map[string]session),
	}
}

// Handler returns the root mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/setup/status", s.handleSetupStatus)
	mux.HandleFunc("POST /api/setup/complete", s.requireAuth(s.handleSetupComplete))
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("GET /api/auth/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("POST /api/auth/password", s.requireAuth(s.handlePassword))
	mux.HandleFunc("GET /api/sources", s.requireAuth(s.handleListSources))
	mux.HandleFunc("POST /api/sources", s.requireAuth(s.handleCreateSource))
	mux.HandleFunc("POST /api/sources/{id}/refresh", s.requireAuth(s.handleRefreshSource))
	mux.HandleFunc("POST /api/sources/{id}/refresh/async", s.requireAuth(s.handleRefreshSourceAsync))
	mux.HandleFunc("GET /api/jobs/refresh/{id}", s.requireAuth(s.handleRefreshJob))
	mux.HandleFunc("GET /api/channels", s.requireAuth(s.handleListChannels))
	mux.HandleFunc("GET /playlist.m3u", s.handlePlaylist)
	mux.HandleFunc("GET /play/{id}", s.handlePlay)
	mux.HandleFunc("GET /api/sessions", s.requireAuth(s.handleSessions))
	mux.HandleFunc("GET /api/cache/stats", s.requireAuth(s.handleCacheStats))
	mux.HandleFunc("POST /api/channels/{id}/promote", s.requireAuth(s.handlePromote))
	mux.HandleFunc("POST /api/library/roots", s.requireAuth(s.handleSetLibraryRoot))
	mux.HandleFunc("GET /api/library/roots", s.requireAuth(s.handleListLibraryRoots))
	mux.HandleFunc("POST /api/emby/config", s.requireAuth(s.handleEmbyConfig))
	mux.HandleFunc("GET /api/emby/status", s.requireAuth(s.handleEmbyStatus))
	mux.HandleFunc("GET /api/emby/folders", s.requireAuth(s.handleEmbyFolders))
	return mux
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	n := 0
	if s.Streams != nil {
		n = s.Streams.ActiveLiveSessions()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"live_fanout_sessions": n,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "smoothie",
	})
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.DB.GetSetupStatus()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.MarkSetupComplete(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	st, err := s.DB.GetSetupStatus()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	u, err := s.DB.Authenticate(req.Username, req.Password)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	tok, err := newToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token")
		return
	}
	s.mu.Lock()
	s.sessions[tok] = session{Username: u.Username, ExpiresAt: time.Now().Add(24 * time.Hour)}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"token":    tok,
		"username": u.Username,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	tok := bearer(r)
	s.mu.Lock()
	delete(s.sessions, tok)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := usernameFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{"username": u})
}

type passwordReq struct {
	Password string `json:"password"`
}

func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	var req passwordReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "password required")
		return
	}
	user := usernameFrom(r.Context())
	if err := s.DB.UpdatePassword(user, req.Password); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.ListSources()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type createSourceReq struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Enabled    *bool  `json:"enabled"`
	Priority   int    `json:"priority"`
	ConfigJSON string `json:"config_json"`
	LimitsJSON string `json:"limits_json"`
}

func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	var req createSourceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	en := true
	if req.Enabled != nil {
		en = *req.Enabled
	}
	src := store.Source{
		Name:       req.Name,
		Type:       req.Type,
		Enabled:    en,
		Priority:   req.Priority,
		ConfigJSON: req.ConfigJSON,
		LimitsJSON: req.LimitsJSON,
	}
	if err := s.DB.CreateSource(&src); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, src)
}
