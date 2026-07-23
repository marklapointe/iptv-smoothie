package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mlapointe/smoothie/internal/api"
	"github.com/mlapointe/smoothie/internal/store"
)

func TestHealth(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestLogin_DefaultAdminAndWizard(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Handler()

	// setup status before login — public
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/setup/status", nil))
	if rec.Code != 200 {
		t.Fatalf("setup status %d %s", rec.Code, rec.Body.String())
	}
	var st store.SetupStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.WizardRequired {
		t.Fatalf("wizard should be required: %+v", st)
	}

	// bad login
	rec = httptest.NewRecorder()
	body := bytes.NewBufferString(`{"username":"admin","password":"nope"}`)
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/login", body))
	if rec.Code != 401 {
		t.Fatalf("want 401 got %d", rec.Code)
	}

	// admin:admin
	rec = httptest.NewRecorder()
	body = bytes.NewBufferString(`{"username":"admin","password":"admin"}`)
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/login", body))
	if rec.Code != 200 {
		t.Fatalf("login %d %s", rec.Code, rec.Body.String())
	}
	var login map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	tok := login["token"]
	if tok == "" {
		t.Fatal("empty token")
	}

	// complete setup
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/setup/complete", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("complete %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if st.WizardRequired {
		t.Fatal("wizard still required after complete")
	}
}

func TestCreateSource_RequiresAuth(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Handler()
	rec := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"name":"A","type":"iptv_m3u"}`)
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/sources", body))
	if rec.Code != 401 {
		t.Fatalf("want 401 got %d", rec.Code)
	}

	tok := loginAdmin(t, h)
	rec = httptest.NewRecorder()
	body = bytes.NewBufferString(`{"name":"A","type":"iptv_m3u","config_json":"{}"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sources", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
}

func newTestServer(t *testing.T) *api.Server {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return api.New(db)
}

func loginAdmin(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"username":"admin","password":"admin"}`)
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/login", body))
	if rec.Code != 200 {
		t.Fatalf("login %d", rec.Code)
	}
	var login map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	return login["token"]
}
