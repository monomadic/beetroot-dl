# beetroot-dl — agent guide

CLI downloader for Beatport (FLAC/AAC), written in pure Go (tagging uses
taglib compiled to WebAssembly via `go.senan.xyz/taglib` — no CGO anywhere).
Fork of `unspok3n/beatportdl`, reworked to behave like a normal unix
tool: results on stdout, diagnostics on stderr, real exit codes, download-and-
exit by default (`-i` for the old interactive loop), TOML config at
`~/.config/beetroot-dl.toml`.

Requires an active Beatport/Beatsource streaming plan to actually download.

## Current state

- `TASKS.md` holds the prioritised backlog. Check it before starting work; tick
  items off as they land. The P0 correctness/security items and the CGO
  removal are done; what remains is the handler deduplication, removing
  `os.Exit` from the input helpers, and more tests.
- CI (`.github/workflows/ci.yml`) runs gofmt, a vendor-sync check, build, vet,
  `go test -race` and golangci-lint on ubuntu + macos. Run `go vet ./...` and
  `go test -race ./...` before pushing; the vendor-sync check means
  a `go.mod` change must be accompanied by `go mod tidy && go mod vendor`.
- Lint config is `.golangci.yml` (pinned to v2.6.2 in CI). errcheck
  exclusions cover deferred `Close`, writes to stderr/the error log, and
  `os.Remove` cleanup — don't add naked error-ignoring beyond those.
- Dependencies are vendored (`vendor/`).

## Layout

| Path | What lives there |
|---|---|
| `main.go` | flag parsing, app struct, signal handling, main loop, `printResults` |
| `downloader.go` | per-link-type handlers (track/release/playlist/chart/label/artist), `saveTrack`, `tagTrack` — the four list handlers are near-duplicates |
| `needledrop.go` | HLS path: m3u8 parsing, AES-CBC segment decryption, ffmpeg remux |
| `interactions.go` | first-run setup wizard, interactive prompts, search, label/artist filter wizard |
| `concurrency.go` | worker/semaphore helpers (`globalSem` for URL handling, `downloadSem` for downloads) |
| `utils.go` | `downloadFile`, progress bars, logging (`LogError`/`LogInfo`/`LogVerbose`), config/cache file discovery |
| `stats.go` | facet stats for the interactive filter wizard |
| `completions.go` | embedded bash/zsh completion scripts, printed by `--completions` (keep in sync with the flags in `main.go` — a test enforces it) |
| `internal/beatport/` | API client: `beatport.go` (`fetch`, auth-aware HTTP), `auth.go` (OAuth password flow + token cache), one file per entity (tracks, releases, playlists, charts, labels, artists), `links.go` (URL parsing) |
| `config/` | TOML config parsing/validation (`config.go`), default tag mappings (`tag_mappings.go`) |
| `vendor/` | vendored deps — keep in sync with `go.mod` |

The module is `github.com/monomadic/beetroot-dl`; the main package is at the
repo root, so `go install github.com/monomadic/beetroot-dl@latest` installs
the binary.

## Building and testing

Pure Go — plain `go build`/`go test` work with no C toolchain or system
libraries. Tagging runs taglib as embedded WebAssembly (wazero), verified by
round-trip tests against real FLAC/M4A fixtures in
`testdata/`.

```bash
go build            # → ./beetroot-dl
go test ./...
go install          # → GOPATH/bin/beetroot-dl
GOOS=... GOARCH=... go build   # cross-compile, no extra setup
```

The main package sits at the repo root, so bare `go build`/`go install`
work. There is no Makefile: `--version` comes from the toolchain's own
build-info stamping (module version for `go install ...@latest`, git
tag/commit + `+dirty` for checkout builds), overridable with
`-ldflags "-X main.version=..."`.

## Conventions

- **stdout is sacred**: only downloaded file paths go there. Everything else —
  progress, errors, prompts, verbose steps — goes to stderr. Never add a
  `fmt.Println` for diagnostics.
- Exit codes: 0 ok, 1 any failure, 2 usage error, 130 interrupted.
- Errors are logged (and counted, for the exit code) via `app.LogError`;
  handlers log-and-return rather than propagate.
- `--version` uses the toolchain's build-info stamping (see Building); don't
  reintroduce ldflags plumbing for it.
- **All network access goes through the `beatport` client.** API calls use
  `fetch`; track/cover/HLS transfers use `app.get`, which wraps
  `bp.DownloadClient()` and binds the request to `app.ctx`. Never reach for
  `http.Get`/`http.DefaultClient` — that bypasses the configured proxy and
  the cancellation path.
- The URL queue is behind `urlsMutex`; use `addUrls`/`snapshotUrls`/
  `clearUrls`/`hasUrls` rather than touching `app.urls` directly (the signal
  handler reads it concurrently).
