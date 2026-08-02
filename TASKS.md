# Tasks

Prioritised work list from the 2026-08-02 codebase assessment. Tick items off
as they land; keep this file current when scope changes.

## P0 — Correctness and security

- [ ] **Route all downloads through one configured HTTP client.**
  `cmd/beetroot-dl/utils.go` (`downloadFile`) and `cmd/beetroot-dl/needledrop.go`
  use bare `http.Get` — no timeout, and they bypass the `proxy` setting that
  the API client in `internal/beatport/beatport.go` honours. Track/cover/HLS
  traffic leaks the real IP when a proxy is configured, and a stalled
  connection hangs forever.
- [ ] **Cap the 401 retry in `fetch`.** `internal/beatport/beatport.go` retries
  recursively on 401 with no bound — a persistent 401 loops forever,
  re-logging-in each time. One retry, then fail.
- [ ] **Fix token data races.** `fetch` reads `auth.tokenPair.AccessToken`
  without the auth mutex; `Auth.Check` drops the read lock before taking the
  write lock, so concurrent workers can all trigger a refresh. Guard the read;
  make refresh single-flight (`resenje.org/singleflight` is already vendored).
- [ ] **Write the config file 0600.** `config.Save` uses `os.Create` (0644) and
  the file contains the plaintext password. The credentials cache already gets
  0600 — match it.
- [ ] **Validate CBC padding in `decryptSegment`** (`needledrop.go`). A
  malformed segment where the padding byte exceeds the segment length panics.
  Also check segment length is a multiple of the AES block size.
- [ ] **Stop racing shared state from the signal handler.** The goroutine in
  `main.go` reads `app.urls` and calls `printResults` while the main loop
  mutates them.

## P1 — Toolchain, dependencies, infrastructure

- [ ] **`go mod tidy` + `go mod vendor`.** `gopkg.in/yaml.v2` is a direct
  dependency nothing imports (left over from the YAML→TOML migration);
  `mattn/go-isatty` is imported directly but marked indirect.
- [ ] **Bump the Go directive** (`go 1.22.5` → current toolchain, 1.26.x) and
  update dependencies: `BurntSushi/toml` 1.4→1.6, `fatih/color` 1.15→1.19,
  `mpb/v8` 8.8→8.14, `grafov/m3u8` 0.12.0→0.12.1, `golang.org/x/sys` 0.28→0.47
  (includes security fixes). All minor bumps.
- [ ] **Make the module path installable.** `monomadic/beetroot-dl` is not
  fetchable; rename to `github.com/monomadic/beetroot-dl` so `go install`
  works.
- [ ] **Add CI.** No workflows exist. GitHub Actions running `go vet`,
  `go test`, `golangci-lint`, and a build on ubuntu + macos (both need the
  system `taglib` package for CGO). Add a release workflow later.
- [ ] **Add tests** for the pure logic: `config.Parse`, the filename/dedup
  logic in `saveTrack`, `ForPaginated`, and date normalisation in
  `interactions.go`. Coverage today is two small util test files.
- [ ] **Native `make build` target.** Every Makefile target requires zig and
  runs `go clean -cache`; a plain host build currently needs manual
  `CGO_CFLAGS`/`CGO_CXXFLAGS` pointing at taglib (e.g. Homebrew's
  `/opt/homebrew/include`).

## P2 — Refactors and smaller fixes

- [ ] **Deduplicate the four ~100-line link handlers** in `downloader.go`
  (`handlePlaylistLink` / `handleChartLink` / `handleLabelLink` /
  `handleArtistLink`) into a shared per-track helper. Copy-paste drift already
  exists — the chart handler's error says "handle playlist items".
- [ ] **Propagate context into HTTP requests** so Ctrl-C cancels in-flight
  downloads instead of waiting for them (`concurrency.go` only checks ctx
  before a worker starts). Pairs with the shared-client P0 item.
- [ ] **Remove `os.Exit` from input helpers.** `GetLine` exits the process from
  deep inside, patched by the global `onExit` hook in `utils.go`. Return
  errors and decide in `main`.
- [ ] **Path handling:** use `filepath.Join` instead of `fmt.Sprintf("%s/%s")`
  in `saveTrack` and `handleCoverFile` (Windows is a build target).
- [ ] **HLS temp file:** created 0755 and leaked on error (`downloadSegments`
  returns `""` on failure so the caller's cleanup removes nothing).
- [ ] **Validate `quality` in `config.Parse`** — a typo currently falls through
  silently to the default download path.
- [ ] **Simplify `getStreamSegments` signature** — returns `*[]string,
  *StreamKey` for no reason.
- [ ] **Strategic: consider dropping CGO taglib** for a pure-Go tagging path
  (only FLAC and M4A are written). Would eliminate the zig/SDK
  cross-compilation machinery, make CI trivial, and turn releases into plain
  `GOOS=... go build`. Largest task here, largest maintenance win.

## Suggested order

1. One mechanical PR: tidy, dependency bumps, module path (P1 items 1–3).
2. One correctness PR: the six P0 items.
3. CI + tests, so regressions get caught.
4. Refactors as appetite allows.
