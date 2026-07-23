# Smoothie

Local IPTV / OTA restream layer: one upstream pull, many LAN viewers; VOD progressive play + cache; optional Emby library import.

## Quick start

```bash
go build -o smoothie ./cmd/smoothie
export SMOOTHIE_LISTEN=127.0.0.1:8787
export SMOOTHIE_DATA_DIR=./data
export SMOOTHIE_DB=./data/smoothie.db
./smoothie
```

Open http://127.0.0.1:8787

| | |
|--|--|
| Default login | `admin` / `admin` |
| First run | **Setup wizard** until configuration is marked complete |
| Health | `GET /api/health` |
| Client playlist | `GET /playlist.m3u` |
| Play | `GET /play/{channel_id}` |

## Stack

- **Go** backend, **GORM + SQLite** (pure Go driver, no CGO)
- Bootstrap admin UI (Angular app planned under `web/`)
- Multi-source IPTV M3U + HDHomeRun (HDHR next)
- Dual packaging: OCI + FreeBSD/Linux bare metal stubs in `deploy/`

## API (auth)

```bash
TOKEN=$(curl -s -X POST localhost:8787/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | jq -r .token)

curl -s -H "Authorization: Bearer $TOKEN" localhost:8787/api/sources
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  localhost:8787/api/sources \
  -d '{"name":"Primary","type":"iptv_m3u","config_json":"{\"urls\":[\"http://portal/get.php?…\"]}","limits_json":"{\"max_concurrent_upstreams\":2,\"max_upstream_bps\":1500000}"}'

curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  localhost:8787/api/sources/{id}/refresh

curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  localhost:8787/api/setup/complete
```

## Lab ingest (local M3U file)

```bash
go run ./cmd/smoothie-lab-ingest -db ./data/smoothie.db -m3u /path/to/playlist.m3u
```

Do **not** commit portal passwords or Emby API keys.

## Tests

```bash
go test ./... -count=1 -timeout 60s
go test ./internal/hub/ ./internal/ratelimit/ -race -count=1
```

## Status

- GORM/SQLite, `admin`/`admin`, setup wizard
- Multi-source IPTV M3U + **HDHomeRun** lineup import
- Live hub fan-out + per-source concurrency limits
- Progressive VOD **cache / purgatory / promote** (movies vs `tv/Season NN`)
- Rewritten `playlist.m3u` + `/play/{id}`
- Bootstrap admin UI with `data-testid` hooks (Playwright-ready)
- Emby API refresh still next; full Angular app still next
