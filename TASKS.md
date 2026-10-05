# Tasks

Prioritised work list, last revised 2026-08-03. Tick items off as they land;
keep this file current when scope changes.

Completed and dropped from this list: the 2026-08-02 P0 correctness/security
items, the toolchain and dependency work, the module rename, CI, the CGO
removal, and the 2026-08-03 documentation and code pass below. See the git
history for what changed.

## P0 — Documentation accuracy

- [x] **Fix the stale `cmd/beetroot-dl/…` paths.** The main package moved to
  the repo root; this file referred to a directory that no longer exists.
- [x] **Resolve the Makefile contradiction.** An earlier entry claimed a native
  `make build` target had landed. There is no Makefile — it went away with the
  CGO removal.
- [x] **One source of truth for status.** `CLAUDE.md`'s "Current state" section
  is gone; it now points here and carries only the invariants that aren't
  backlog items.
- [x] **Correct the drift note in `CLAUDE.md`.** Both the chart and label
  handlers logged `"handle playlist items"`; fixed by the deduplication below.

## P1 — Make the agent guide load-bearing

- [x] **"How it flows"** — a diagram of arg → parse → dispatch → paginate →
  save → tag, and where the two semaphores apply.
- [x] **"Common changes" recipes** for adding a flag, a config option, a URL
  type, a template keyword and a tag mapping, each naming the test that
  enforces the coupling.
- [x] **Search and build block** — exclude `vendor/` from searches, plus the
  pre-push sequence mirroring `.github/workflows/ci.yml`.
- [x] **Document the exit contract** — input helpers return errors; the
  `onExit` global is gone.
- [x] **Non-goals section** — no CGO, no Makefile, no ldflags `--version`, no
  widening the errcheck exclusions, no config beside the binary.
- [x] **Document `testdata/` and what can actually be tested** without live
  Beatport credentials.
- [x] **Note the `AGENTS.md` symlink** and add a last-verified anchor.

## P2 — Code

- [x] **Deduplicate the four list handlers.** `handlePlaylistLink` /
  `handleChartLink` / `handleLabelLink` / `handleArtistLink` now describe
  themselves as `trackList` values and share one `handleTrackList` pipeline.
  `downloader.go` went 862 → 760 lines.
- [x] **Test `internal/beatport/links.go`** — table-driven over all six URL
  types, locale/catalog prefixes and malformed input.
- [x] **Add `httptest`-backed client tests** pinning the 401 retry cap, the
  post-reauth retry, single-flight refresh under concurrency, and the
  no-retry rule for auth endpoints.
- [x] **Remove `os.Exit` from the input helpers.** `GetLine` returns
  `(string, error)`; `exitNow` and the `onExit` global are deleted.
- [x] **Test the remaining pure logic** — filename dedup, `ForPaginated`,
  `normaliseDate*`, `rankMap`/`newEntityStats`, `GetLine`.
- [x] **Add a release workflow.** `.github/workflows/release.yml`
  cross-compiles five targets on a `v*` tag and publishes with checksums.
- [x] **Extend `config.Parse` validation** — worker counts, `artists_limit`,
  `cover_size`, and every template placeholder (validated against keywords
  derived from the entity `templateValues` maps, so the two cannot drift).

### Bugs found and fixed while doing the above

- [x] **Deadlock on every token refresh.** `Auth.Check` held the `tokenPair`
  write lock across the refresh request, and every request reads the token
  back through `AccessToken`'s `RLock` — Go's `RWMutex` is not reentrant, so
  any run whose cached token was within 5 minutes of expiry hung forever on
  the first API call. Refreshes are now serialised by a separate
  `refreshMutex` and the token lock is never held across I/O.
- [x] **Panic on `/library` URLs.** `ParseUrl` indexed `segments[1]` without
  checking the length.
- [x] **Panic on a single-element `bpm_range`.** `Playlist.DirectoryName`
  checked `len > 0` then indexed `[1]`.
- [x] **Abandoned in-flight downloads.** A pagination error returned from a
  list handler without waiting on its `WaitGroup`, so the run could tear down
  the progress bars while workers were still writing.
- [x] **Label covers ignored `keep_cover`/`fix_tags`.** The label handler
  downloaded cover art unconditionally; the shared pipeline applies the same
  `requireCover` gate as everything else.
- [x] **`Pause` prompted on stdout**, violating the stdout-is-sacred rule.

## P3 — Product direction

- [ ] **`--json` output mode.** stdout is already machine-only; one JSON object
  per downloaded track makes the tool a first-class pipeline citizen.
- [ ] **`--dry-run`.** Resolve, print the target paths and filenames, download
  nothing. Also the missing manual-test path for template changes.
- [ ] **429 handling and backoff** in `fetch`.
- [ ] **Resumable downloads** for large batches.

## Also worth doing

- [x] **Prototype a static C archive for Rust integration.** Optional
  `beetroot_library` tag, persistent clients, request handles, API/transfer
  cancellation, polling progress, shared direct-download/tagging code.
  See `docs/C_LIBRARY.md`; VirtualDJ runtime lifetime remains a separate gate.

- [ ] **Fetch fewer full tracks in list handlers.** `handleListTrack` calls
  `GetTrack` for every id even when the list response already carried most of
  the track. It matches the old behaviour (all four handlers did this), but a
  playlist of 200 tracks is 200 avoidable requests if the list payload is
  sufficient.
- [ ] **Cache releases across pages.** `singleflight` dedupes concurrent
  fetches only; sequential pages of the same label refetch the same release.
- [ ] **Cover `saveTrack`'s quality/extension switch** — it is pure branching
  on `StreamQuality` and currently untested.
