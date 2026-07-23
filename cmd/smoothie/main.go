package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/mlapointe/smoothie/internal/api"
	"github.com/mlapointe/smoothie/internal/config"
	"github.com/mlapointe/smoothie/internal/store"
)

func main() {
	cfg := config.Default()
	if err := cfg.EnsureDataDir(); err != nil {
		log.Fatalf("data dir: %v", err)
	}
	// Ensure parent of DB path exists
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o750); err != nil {
		log.Fatalf("db dir: %v", err)
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer db.Close()

	srv := api.New(db)
	srv.BaseURL = "http://" + cfg.ListenAddr
	apiHandler := srv.Handler()
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") ||
			r.URL.Path == "/playlist.m3u" ||
			strings.HasPrefix(r.URL.Path, "/play/") {
			apiHandler.ServeHTTP(w, r)
			return
		}
		if st, err := os.Stat(cfg.StaticDir); err == nil && st.IsDir() {
			spaFallback(cfg.StaticDir, http.FileServer(http.Dir(cfg.StaticDir))).ServeHTTP(w, r)
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(bootstrapHTML))
	})

	log.Printf("smoothie listening on http://%s (default login admin:admin)", cfg.ListenAddr)
	st, _ := db.GetSetupStatus()
	if st != nil && st.WizardRequired {
		log.Printf("setup wizard required — open UI and complete configuration")
	}
	if err := http.ListenAndServe(cfg.ListenAddr, root); err != nil {
		log.Fatal(err)
	}
}

func spaFallback(root string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(root, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			next.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
	})
}

// Minimal bootstrap page until Angular app is built; surfaces wizard + login.
const bootstrapHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <title>Smoothie Setup</title>
  <style>
    :root { font-family: system-ui, sans-serif; color: #e8eef7; background: #0f1419; }
    body { max-width: 40rem; margin: 2rem auto; padding: 0 1rem; }
    h1 { font-weight: 600; }
    .card { background: #1a2332; border-radius: 12px; padding: 1.25rem; margin: 1rem 0; }
    label { display: block; margin: 0.5rem 0 0.2rem; font-size: 0.9rem; color: #9db0c9; }
    input, select { width: 100%; padding: 0.5rem; border-radius: 8px; border: 1px solid #2a3a52; background: #0f1419; color: inherit; box-sizing: border-box; }
    button { margin-top: 0.75rem; padding: 0.55rem 1rem; border: 0; border-radius: 8px; background: #3b82f6; color: white; font-weight: 600; cursor: pointer; }
    button.secondary { background: #334155; }
    .muted { color: #9db0c9; font-size: 0.9rem; }
    .err { color: #f87171; }
    .ok { color: #4ade80; }
    .banner { background: #422006; border: 1px solid #f59e0b; color: #fde68a; padding: 0.75rem 1rem; border-radius: 8px; }
    code { background: #0f1419; padding: 0.1rem 0.35rem; border-radius: 4px; }
  </style>
</head>
<body>
  <h1>Smoothie</h1>
  <p class="muted">IPTV / OTA restream layer</p>
  <div id="wizard-banner" class="banner" hidden data-testid="wizard-banner">
    Setup wizard required — the system is not fully configured yet.
  </div>
  <div class="card" id="login-card" data-testid="login-card">
    <h2>Sign in</h2>
    <p class="muted">Default account: <code>admin</code> / <code>admin</code></p>
    <label>Username</label>
    <input id="user" value="admin" data-testid="login-username"/>
    <label>Password</label>
    <input id="pass" type="password" value="admin" data-testid="login-password"/>
    <button id="login-btn" data-testid="login-submit">Sign in</button>
    <p id="login-msg" class="err" data-testid="login-msg"></p>
  </div>
  <div class="card" id="wizard-card" hidden data-testid="wizard-card">
    <h2>Setup wizard</h2>
    <p class="muted">Add a source and finish setup. You can change the admin password now.</p>
    <label>New password (optional)</label>
    <input id="newpass" type="password" data-testid="wizard-new-password" placeholder="leave blank to keep admin"/>
    <label>First IPTV source name</label>
    <input id="src-name" value="Primary IPTV" data-testid="wizard-source-name"/>
    <label>M3U / portal URL</label>
    <input id="src-url" placeholder="http://…/get.php?…" data-testid="wizard-source-url"/>
    <button id="finish-btn" data-testid="wizard-finish">Save &amp; finish setup</button>
    <button class="secondary" id="skip-btn" data-testid="wizard-skip-finish">Finish without source</button>
    <p id="wizard-msg" data-testid="wizard-msg"></p>
  </div>
  <div class="card" id="app-card" hidden data-testid="app-card">
    <h2>Dashboard</h2>
    <p class="ok">Configured. Angular UI will replace this shell.</p>
    <pre id="status" data-testid="status-json"></pre>
  </div>
<script>
let token = localStorage.getItem('smoothie_token') || '';
async function api(path, opts={}) {
  const headers = Object.assign({'Content-Type':'application/json'}, opts.headers||{});
  if (token) headers['Authorization'] = 'Bearer ' + token;
  const res = await fetch(path, Object.assign({}, opts, {headers}));
  const text = await res.text();
  let data; try { data = JSON.parse(text); } catch { data = {raw:text}; }
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}
async function refresh() {
  const st = await api('/api/setup/status');
  document.getElementById('wizard-banner').hidden = !st.wizard_required;
  document.getElementById('status').textContent = JSON.stringify(st, null, 2);
  if (!token) {
    document.getElementById('login-card').hidden = false;
    document.getElementById('wizard-card').hidden = true;
    document.getElementById('app-card').hidden = true;
    return;
  }
  document.getElementById('login-card').hidden = true;
  if (st.wizard_required) {
    document.getElementById('wizard-card').hidden = false;
    document.getElementById('app-card').hidden = true;
  } else {
    document.getElementById('wizard-card').hidden = true;
    document.getElementById('app-card').hidden = false;
  }
}
document.getElementById('login-btn').onclick = async () => {
  try {
    const data = await api('/api/auth/login', {method:'POST', body: JSON.stringify({
      username: document.getElementById('user').value,
      password: document.getElementById('pass').value,
    })});
    token = data.token; localStorage.setItem('smoothie_token', token);
    document.getElementById('login-msg').textContent = '';
    await refresh();
  } catch (e) {
    document.getElementById('login-msg').textContent = e.message;
  }
};
async function finish(withSource) {
  const msg = document.getElementById('wizard-msg');
  try {
    const np = document.getElementById('newpass').value;
    if (np) await api('/api/auth/password', {method:'POST', body: JSON.stringify({password: np})});
    if (withSource) {
      const url = document.getElementById('src-url').value.trim();
      const name = document.getElementById('src-name').value.trim() || 'Primary IPTV';
      if (!url) { msg.className='err'; msg.textContent='URL required (or finish without source)'; return; }
      await api('/api/sources', {method:'POST', body: JSON.stringify({
        name, type: 'iptv_m3u',
        config_json: JSON.stringify({urls:[url]}),
        limits_json: JSON.stringify({max_concurrent_upstreams:2,max_upstream_bps:1500000}),
      })});
    }
    await api('/api/setup/complete', {method:'POST', body:'{}'});
    msg.className='ok'; msg.textContent='Setup complete';
    await refresh();
  } catch (e) {
    msg.className='err'; msg.textContent = e.message;
  }
}
document.getElementById('finish-btn').onclick = () => finish(true);
document.getElementById('skip-btn').onclick = () => finish(false);
refresh().catch(e => { document.getElementById('login-msg').textContent = e.message; });
</script>
</body>
</html>
`
