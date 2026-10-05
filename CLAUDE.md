# beetroot-dl — agent guide

*Last verified against 9eb566a + the 2026-08-03 refactor. If something below
disagrees with the code, the code wins — fix this file.*

CLI downloader for Beatport (FLAC/AAC), written in pure Go (tagging uses
taglib compiled to WebAssembly via `go.senan.xyz/taglib` — no CGO anywhere).
Fork of `unspok3n/beatportdl`, reworked to behave like a normal unix
tool: results on stdout, diagnostics on stderr, real exit codes, download-and-
exit by default (`-i` for the old interactive loop), TOML config at
`~/.config/beetroot-dl.toml`.

Requires an active Beatport/Beatsource streaming plan to actually download.

`AGENTS.md` is a symlink to this file — editing either edits both. Some tools
do not follow symlinks, so read this path directly if `AGENTS.md` looks empty.

## Backlog

`TASKS.md` is the single source of truth for what is outstanding. Check it
before starting work and tick items off as they land. Do not mirror its
contents here — two copies of the status is what let this file and `TASKS.md`
drift apart in the first place.

## Layout

| Path | What lives there |
|---|---|
| `main.go` | flag parsing, app struct, signal handling, main loop, `printResults` |
| `downloader.go` | per-link-type handlers, the shared `trackList` pipeline, `saveTrack`, `tagTrack` |
| `needledrop.go` | HLS path: m3u8 parsing, AES-CBC segment decryption, ffmpeg remux |
| `interactions.go` | first-run setup wizard, interactive prompts, search, label/artist filter wizard |
| `concurrency.go` | worker/semaphore helpers (`globalSem` for URL handling, `downloadSem` for downloads) |
| `utils.go` | `downloadFile`, progress bars, logging (`LogError`/`LogInfo`/`LogVerbose`), `GetLine`, config/cache file discovery |
| `stats.go` | facet stats for the interactive filter wizard |
| `completions.go` | embedded bash/zsh completion scripts, printed by `--completions` (keep in sync with the flags in `main.go` — a test enforces it) |
| `internal/beatport/` | API client: `beatport.go` (`fetch`, auth-aware HTTP), `auth.go` (OAuth password flow + token cache), one file per entity, `links.go` (URL parsing), `templates.go` (template keyword registry) |
| `config/` | TOML config parsing/validation (`config.go`), default tag mappings (`tag_mappings.go`) |
| `vendor/` | vendored deps — keep in sync with `go.mod` |

The module is `github.com/monomadic/beetroot-dl`; the main package is at the
repo root, so `go install github.com/monomadic/beetroot-dl@latest` installs
the binary.

**Read order for a change in the download path:** `main.go` (how a run is set
up) → `downloader.go:handleUrl` (dispatch) → the specific handler. For an
auth or transport change, `internal/beatport/beatport.go` and `auth.go` —
that pair is where every correctness bug so far has lived.

## How it flows

```
arg ──► app.addUrls (behind urlsMutex)
     ──► globalWorker (globalSem) ──► handleUrl
                                        └─ bp.ParseUrl → *beatport.Link
                                           └─ switch link.Type
       ┌────────────────────────────────────┴───────────────────────────┐
   handleTrackLink / handleReleaseLink                 handleTrackList  (playlist,
   (one-off shapes)                                     chart, label, artist)
                                                          │
                                       ForPaginated ──► yield(trackID)
                                                          │
                                       downloadWorker (downloadSem)
                                                          │
                                                    handleListTrack
                                                          │
        GetTrack → singleflight GetRelease → per-release dir? → cover →
        handleTrack → saveTrack (download/HLS) → tagTrack → recordDownload
```

Everything printed to stdout comes from `recordDownload` via `printResults`.

The four list-shaped links share one pipeline: a handler fetches its entity,
then hands `handleTrackList` a `trackList` describing how to enumerate the
tracks and how files should be laid out. Adding a fifth list type is a ~15-line
handler, not a copy of the pipeline.

## Common changes

**Add a flag.** `main.go` (declare + parse + the `usageText` block) →
`completions.go` (both the bash and zsh scripts) → the README flag list.
`completions_test.go` fails if a flag in `main.go` is missing from the scripts.

**Add a config option.** `config/config.go` (struct field with a `toml` tag,
default in `Defaults()`, validation before `Parse` returns) → the README
options table. The default must go in `Defaults()`, not inline in `Parse`: the
first-run wizard builds its config from the same function, and a field missing
there gets written out as a zero value that overrides the default on the next
load — and now fails validation. Anything unvalidated fails late and
confusingly, so validate it there; `config_test.go` has a table of rejection
cases to extend.

**Add a URL type.** `internal/beatport/links.go` (`LinkType` + a `ParseUrl`
case, with cases in `links_test.go`) → an entity file in `internal/beatport/`
→ a handler in `downloader.go` (for a paginated list, a `trackList` value —
see `handlePlaylistLink`) → `handleUrl`'s switch.

**Add a template keyword.** Add it to the entity's `templateValues` method in
`internal/beatport/`. That is the only step: `templates.go` derives the valid
keyword list from those maps, so config validation and the formatter cannot
disagree. Then document it in the README.

**Add a tag mapping.** `config/tag_mappings.go` (default map + the validation
list) and `tagTrack` in `downloader.go` if the value needs computing.

## Building and testing

Pure Go — plain `go build`/`go test` work with no C toolchain or system
libraries. Tagging runs taglib as embedded WebAssembly (wazero), verified by
round-trip tests against real FLAC/M4A fixtures in `testdata/`
(`silence.flac`, `silence.m4a`, `cover.jpg` — small real files, so the taglib
round trip is exercised rather than mocked).

```bash
go build            # → ./beetroot-dl
go test ./...
go install          # → GOPATH/bin/beetroot-dl
GOOS=... GOARCH=... go build   # cross-compile, no extra setup
```

Before pushing, run what CI runs:

```bash
gofmt -l $(git ls-files '*.go' | grep -v '^vendor/')   # must print nothing
go vet ./...
go test -race ./...
go mod tidy && go mod vendor                            # only if go.mod changed
```

`.github/workflows/ci.yml` runs exactly that on ubuntu + macos, plus
golangci-lint (config in `.golangci.yml`, pinned to v2.6.2). The vendor-sync
check means a `go.mod` change must be accompanied by `go mod tidy && go mod
vendor`. `.github/workflows/release.yml` cross-compiles the five published
targets on a `v*` tag.

**Searching:** always exclude `vendor/` — it is far larger than the real tree
and will bury a symbol in dependency noise. Prefer `git ls-files` to a bare
recursive grep:

```bash
grep -rn "handleTrackList" $(git ls-files '*.go')
```

### What can and cannot be tested

There are no HTTP fakes for the download path: every handler needs live
Beatport credentials, so `handleUrl` and below are not covered. What *is*
testable, and where the existing tests live:

- `internal/beatport/client_test.go` — an `httptest` server driving `fetch`.
  Point a client at it by setting the unexported `baseUrl` field. Use this for
  anything touching retry, auth or pagination.
- Pure logic: URL parsing, templates, config validation, date normalisation,
  filename dedup, `ForPaginated`, `decryptSegment`, tag round-trips.
- Prompts: `GetLine` reads the package-level `stdin` reader, which a test can
  swap (see `utils_test.go`).

## Conventions

- **stdout is sacred**: only downloaded file paths go there. Everything else —
  progress, errors, prompts, verbose steps — goes to stderr. Never add a
  `fmt.Println` for diagnostics.
- Exit codes: 0 ok, 1 any failure, 2 usage error, 130 interrupted.
- Errors are logged (and counted, for the exit code) via `app.LogError`;
  handlers log-and-return rather than propagate.
- **Input helpers return errors, they do not exit.** `GetLine` returns
  `io.EOF` when the user quits with Ctrl-D, and `main` decides what that
  means. Do not reintroduce an exit from inside a prompt: the previous version
  needed a package-global `onExit` hook purely to get results printed on the
  way out. `Fatal`/`FatalError`/`Pause` are the deliberate exceptions, and
  are only for genuinely unrecoverable startup failures.
- **All network access goes through the `beatport` client.** API calls use
  `fetch`; track/cover/HLS transfers use `app.get`, which wraps
  `bp.DownloadClient()` and binds the request to `app.ctx`. Never reach for
  `http.Get`/`http.DefaultClient` — that bypasses the configured proxy and
  the cancellation path.
- **Never hold `Auth.mutex` across a network call.** It guards `tokenPair`
  only. Requests read the token back through `AccessToken`, whose `RLock`
  cannot be taken while the write lock is held, so doing so deadlocks the
  process. Refreshes are serialised by the separate `refreshMutex`.
- The URL queue is behind `urlsMutex`; use `addUrls`/`snapshotUrls`/
  `clearUrls`/`hasUrls` rather than touching `app.urls` directly (the signal
  handler reads it concurrently).
- A worker that fails must still let its siblings finish. Do not return from a
  handler while its `WaitGroup` has outstanding downloads — the run would tear
  down the progress bars underneath them.

## Non-goals

Decisions already made and paid for; do not undo them without a reason:

- **No CGO in the normal CLI.** The optional `beetroot_library` prototype
  deliberately uses CGO for C exports (see `docs/C_LIBRARY.md`).
  Tagging is WebAssembly taglib precisely so that `go install`
  works on a bare machine and cross-compiling needs no C toolchain.
- **No Makefile**, no zig, no build scripts. `go build` is the build.
- **No ldflags plumbing for `--version`.** It comes from the toolchain's own
  build-info stamping (module version for `go install ...@latest`, git
  tag/commit + `+dirty` for checkout builds). `-ldflags "-X main.version=..."`
  still overrides it, but nothing in the repo should depend on that.
- **No widening the errcheck exclusions** in `.golangci.yml`. They cover
  deferred `Close`, writes to stderr/the error log, and `os.Remove` cleanup.
  Anything else should handle its error.
- **No config next to the binary.** New configs are always created in
  `ConfigDir()`; an existing one elsewhere is honoured, but not created.
