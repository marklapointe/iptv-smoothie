# Lab results

## Full M3U ingest (real provider catalog)

| Metric | Value |
|--------|--------|
| Source file | `/tmp/smoothie-lab/playlist.m3u` (~88 MB) |
| Entries ingested | **319,261** |
| Live | 50,280 |
| VOD | 268,981 |
| Wall time | **~55 s** |
| SQLite DB size | ~285 MB |
| Date | 2026-07-23 |

```bash
go run ./cmd/smoothie-lab-ingest \
  -db ./data/smoothie.db \
  -m3u /path/to/playlist.m3u \
  -name "Primary"
```

### Notes

- Duplicate M3U rows (same URL+name+group) are skipped within a refresh.
- `remote_key` hashes path + name + group so identical stream URLs with different titles do not collide.
- Prefer async refresh in production UI: `POST /api/sources/{id}/refresh/async`.

## Playwright

```bash
cd web/e2e && npm install && npx playwright install chromium
npx playwright test
```
