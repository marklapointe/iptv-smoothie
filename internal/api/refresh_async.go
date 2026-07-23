package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mlapointe/smoothie/internal/source"
)

// refreshJob tracks async M3U/HDHR refresh.
type refreshJob struct {
	ID        string    `json:"id"`
	SourceID  string    `json:"source_id"`
	State     string    `json:"state"` // running|done|error
	Error     string    `json:"error,omitempty"`
	Total     int       `json:"total,omitempty"`
	Live      int       `json:"live,omitempty"`
	VOD       int       `json:"vod,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
}

var (
	jobsMu sync.Mutex
	jobs   = map[string]*refreshJob{}
)

func (s *Server) handleRefreshSourceAsync(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("id")
	// sync path remains POST .../refresh; async is POST .../refresh?async=1 or /refresh/async
	job := &refreshJob{
		ID:        uuid.NewString(),
		SourceID:  sourceID,
		State:     "running",
		StartedAt: time.Now().UTC(),
	}
	jobsMu.Lock()
	jobs[job.ID] = job
	jobsMu.Unlock()

	go func() {
		ref := source.NewRefresher(s.DB)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		res, err := ref.RefreshSource(ctx, sourceID)
		jobsMu.Lock()
		defer jobsMu.Unlock()
		j := jobs[job.ID]
		j.EndedAt = time.Now().UTC()
		if err != nil {
			j.State = "error"
			j.Error = err.Error()
			return
		}
		j.State = "done"
		j.Total = res.Total
		j.Live = res.Live
		j.VOD = res.VOD
	}()

	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleRefreshJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	jobsMu.Lock()
	j, ok := jobs[id]
	if ok {
		cp := *j
		jobsMu.Unlock()
		writeJSON(w, http.StatusOK, cp)
		return
	}
	jobsMu.Unlock()
	writeErr(w, http.StatusNotFound, "job not found")
}
