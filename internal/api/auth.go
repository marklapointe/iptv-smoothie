package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

type ctxKey int

const userKey ctxKey = 1

func usernameFrom(ctx context.Context) string {
	v, _ := ctx.Value(userKey).(string)
	return v
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := bearer(r)
		if tok == "" {
			writeErr(w, http.StatusUnauthorized, "missing token")
			return
		}
		s.mu.Lock()
		sess, ok := s.sessions[tok]
		if ok && time.Now().After(sess.ExpiresAt) {
			delete(s.sessions, tok)
			ok = false
		}
		s.mu.Unlock()
		if !ok {
			writeErr(w, http.StatusUnauthorized, "invalid token")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, sess.Username)
		next(w, r.WithContext(ctx))
	}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	// Also accept cookie for browser UI later
	if c, err := r.Cookie("smoothie_token"); err == nil {
		return c.Value
	}
	return ""
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
