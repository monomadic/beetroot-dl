# beetroot-dl — agent guide

CLI downloader for Beatport (FLAC/AAC), written in Go with a CGO dependency on
taglib. Fork of `unspok3n/beatportdl`, reworked to behave like a normal unix
tool: results on stdout, diagnostics on stderr, real exit codes, download-and-
exit by default (`-i` for the old interactive loop), TOML config at
`~/.config/beetroot-dl.toml`.

Requires an active Beatport/Beatsource streaming plan to actually download.

## Current state

- `TASKS.md` holds the prioritised backlog. Check it before starting work; tick
  items off as they land. The P0 correctness/security items are done; what
  remains is the handler deduplication, removing `os.Exit` from the input
  helpers, more tests, and the strategic question of dropping the CGO taglib
  dependency.
- CI (`.github/workflows/ci.yml`) runs gofmt, a vendor-sync check, build, vet,
  `go test -race` and golangci-lint on ubuntu + macos. Run `make vet` and
  `make test GO_TEST_FLAGS=-race` before pushing; the vendor-sync check means
  a `go.mod` change must be accompanied by `go mod tidy && go mod vendor`.
- Lint config is `.golangci.yml` (pinned to v2.6.2 in CI). errcheck
  exclusions cover deferred `Close`, writes to stderr/the error log, and
  `os.Remove` cleanup — don't add naked error-ignoring beyond those.
- Dependencies are vendored (`vendor/`).

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
| `cmd/beetroot-dl/completions.go` | embedded bash/zsh completion scripts, printed by `--completions` (keep in sync with the flags in `main.go` — a test enforces it) |
| `internal/beatport/` | API client: `beatport.go` (`fetch`, auth-aware HTTP), `auth.go` (OAuth password flow + token cache), one file per entity (tracks, releases, playlists, charts, labels, artists), `links.go` (URL parsing) |
| `internal/taglib/` | CGO binding to taglib (`tag_c.h` + `extensions.cpp`); tags FLAC and M4A |
| `config/` | TOML config parsing/validation (`config.go`), default tag mappings (`tag_mappings.go`) |
| `vendor/` | vendored deps — keep in sync with `go.mod` |

Import path note: the module is `monomadic/beetroot-dl` (not yet a fetchable
path — see TASKS.md).

## Building and testing

CGO + taglib means a plain `go build ./...` fails unless taglib headers are
findable. Use the native Makefile targets, which locate taglib via
pkg-config (falling back to the Homebrew prefix):

```bash
make build     # → ./bin/beetroot-dl
make test      # go test ./... with the right CGO env
make install   # go install into GOPATH/bin
```

The remaining Makefile targets (`all`, `darwin-arm64`, …) are zig-based
cross-compiles for release binaries (see `.env.example` for the static lib
paths they expect) — don't use them for a quick local build. Note the main
package is `./cmd/beetroot-dl`; a bare `go install` at the repo root finds
no Go files.

## Conventions

- **stdout is sacred**: only downloaded file paths go there. Everything else —
  progress, errors, prompts, verbose steps — goes to stderr. Never add a
  `fmt.Println` for diagnostics.
- Exit codes: 0 ok, 1 any failure, 2 usage error, 130 interrupted.
- Errors are logged (and counted, for the exit code) via `app.LogError`;
  handlers log-and-return rather than propagate.
- Version is stamped via `-ldflags -X main.version=...` from git describe.
- **All network access goes through the `beatport` client.** API calls use
  `fetch`; track/cover/HLS transfers use `app.get`, which wraps
  `bp.DownloadClient()` and binds the request to `app.ctx`. Never reach for
  `http.Get`/`http.DefaultClient` — that bypasses the configured proxy and
  the cancellation path.
- The URL queue is behind `urlsMutex`; use `addUrls`/`snapshotUrls`/
  `clearUrls`/`hasUrls` rather than touching `app.urls` directly (the signal
  handler reads it concurrently).
