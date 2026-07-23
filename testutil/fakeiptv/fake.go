package fakeiptv

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
)

// Server is a tiny fake IPTV portal for tests.
type Server struct {
	M3U           string
	LivePayload   []byte
	UpstreamOpens atomic.Int64
	handler       http.Handler
}

// New returns a started httptest server.
func New(m3u string, live []byte) *httptest.Server {
	s := &Server{M3U: m3u, LivePayload: live}
	mux := http.NewServeMux()
	mux.HandleFunc("/get.php", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-mpegURL")
		_, _ = w.Write([]byte(s.M3U))
	})
	mux.HandleFunc("/live/", func(w http.ResponseWriter, r *http.Request) {
		s.UpstreamOpens.Add(1)
		w.Header().Set("Content-Type", "video/mp2t")
		_, _ = w.Write(s.LivePayload)
	})
	mux.HandleFunc("/vod/", func(w http.ResponseWriter, r *http.Request) {
		s.UpstreamOpens.Add(1)
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("ftypisomfake-mp4-bytes"))
	})
	s.handler = mux
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// rewrite absolute URLs in m3u if needed — tests inject server URL
		mux.ServeHTTP(w, r)
	}))
}

// PortalURL builds a get.php URL for the fake server.
func PortalURL(base string) string {
	return fmt.Sprintf("%s/get.php?username=u&password=p&type=m3u_plus&output=ts", base)
}
