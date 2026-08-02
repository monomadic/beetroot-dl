# beetroot-dl — agent guide

CLI downloader for Beatport (FLAC/AAC), written in Go with a CGO dependency on
taglib. Fork of `unspok3n/beatportdl`, reworked to behave like a normal unix
tool: results on stdout, diagnostics on stderr, real exit codes, download-and-
exit by default (`-i` for the old interactive loop), TOML config at
`~/.config/beetroot-dl.toml`.

Requires an active Beatport/Beatsource streaming plan to actually download.

## Current state

- `TASKS.md` holds the prioritised backlog (P0 correctness bugs → P1
  toolchain/CI → P2 refactors). Check it before starting work; tick items off
  as they land.
- No CI yet. Tests are sparse (two util test files).
- Dependencies are vendored (`vendor/`); after changing `go.mod` run
  `go mod tidy && go mod vendor`.

## Layout

| Path | What lives there |
|---|---|
| `cmd/beetroot-dl/main.go` | flag parsing, app struct, signal handling, main loop, `printResults` |
| `cmd/beetroot-dl/downloader.go` | per-link-type handlers (track/release/playlist/chart/label/artist), `saveTrack`, `tagTrack` — the four list handlers are near-duplicates |
| `cmd/beetroot-dl/needledrop.go` | HLS path: m3u8 parsing, AES-CBC segment decryption, ffmpeg remux |
| `cmd/beetroot-dl/interactions.go` | first-run setup wizard, interactive prompts, search, label/artist filter wizard |
| `cmd/beetroot-dl/concurrency.go` | worker/semaphore helpers (`globalSem` for URL handling, `downloadSem` for downloads) |
| `cmd/beetroot-dl/utils.go` | `downloadFile`, progress bars, logging (`LogError`/`LogInfo`/`LogVerbose`), config/cache file discovery |
| `cmd/beetroot-dl/stats.go` | facet stats for the interactive filter wizard |
| `internal/beatport/` | API client: `beatport.go` (`fetch`, auth-aware HTTP), `auth.go` (OAuth password flow + token cache), one file per entity (tracks, releases, playlists, charts, labels, artists), `links.go` (URL parsing) |
| `internal/taglib/` | CGO binding to taglib (`tag_c.h` + `extensions.cpp`); tags FLAC and M4A |
| `config/` | TOML config parsing/validation (`config.go`), default tag mappings (`tag_mappings.go`) |
| `vendor/` | vendored deps — keep in sync with `go.mod` |

Import path note: the module is `monomadic/beetroot-dl` (not yet a fetchable
path — see TASKS.md).

## Building and testing

CGO + taglib means a plain `go build ./...` fails unless taglib headers are
findable. On this machine (Homebrew):

```bash
CGO_CFLAGS="-I/opt/homebrew/include" CGO_CXXFLAGS="-I/opt/homebrew/include" CGO_LDFLAGS="-L/opt/homebrew/lib" go build ./...
```

Same env for `go test ./...`. The Makefile targets are all zig-based
cross-compiles for release binaries (see `.env.example` for the static lib
paths they expect) — don't use them for a quick local build.

## Conventions

- **stdout is sacred**: only downloaded file paths go there. Everything else —
  progress, errors, prompts, verbose steps — goes to stderr. Never add a
  `fmt.Println` for diagnostics.
- Exit codes: 0 ok, 1 any failure, 2 usage error, 130 interrupted.
- Errors are logged (and counted, for the exit code) via `app.LogError`;
  handlers log-and-return rather than propagate.
- Version is stamped via `-ldflags -X main.version=...` from git describe.
