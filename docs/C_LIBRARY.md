# Static C library prototype

The optional `beetroot_library` build tag exposes a C ABI over the existing
Go downloader. It shares API/authentication, filename generation, direct
FLAC/AAC transfers and tagging with the CLI. The normal CLI still builds
without CGO. This prototype does not install or replace anything.

## Build

```sh
CGO_ENABLED=1 go build -tags beetroot_library -buildmode=c-archive -o build/libbeetroot.a .
```

This produces a static archive and a generated C header. `include/beetroot.h`
is the concise public interface. A C compiler is required for this optional
target. Go's c-archive mode does not execute the CLI `main` function, so no
CLI argument parsing, signal handler or process-exit path is entered.

The sibling `virtualdj-plugin-beatport/prototype/beetroot-static` project builds
this archive automatically and links it into a Rust executable. Its build
currently targets the native macOS architecture only; arm64 has been tested.

## API and ownership

- `BrVersion()` returns ABI version 1.
- `BrOpen(config_path, downloads_dir, error_out)` returns a client handle,
  or zero and an allocated error string. Null/empty paths mean default config
  and its configured download directory respectively. An override directory
  is useful for tests. Config must already exist; the library never prompts.
  Client creation performs no network access. Authentication happens lazily
  on the first operation, reusing cached tokens if available.
- `BrRequest(client)` creates a single-use request handle, or zero for an
  invalid client. Allocate before starting a worker so cancellation can never
  miss a request that has not started yet.
- `BrSearch(request, query)` and `BrDownload(request, track_url)` are blocking
  calls. Use a worker thread, never the UI/audio thread. Each request runs
  exactly one operation. Requests on one client serialize; a cancelled queued
  request returns without waiting for the active operation to finish.
- Operation results are allocated UTF-8 JSON: `{"ok":true,"value":...}` or
  `{"ok":false,"error":"..."}`. Search value is an array of the same flat track
  records as CLI JSON search; download value is the final local path.
- `BrProgress(request)` uses the same envelope, with a value containing
  `state`, `bytes_done`, `bytes_total`. Total is -1 before a transfer or when
  unknown. Counts describe the latest transfer (cover or audio), not all
  transfers combined. Polling is safe concurrently with the worker.
- `BrCancel(request)` requests cancellation and is safe from another thread.
  API/auth requests and media transfers use the request context. Synchronous
  tagging must finish before cancellation can return; it is not preempted.
- `BrCloseRequest(request)` cancels and removes its handle. An already-running
  operation retains its state until it returns, so this cannot free underneath
  it. Future calls using that handle fail.
- `BrClose(client)` removes the client handle. Existing requests retain their
  client; release them individually. HTTP pools are shared across requests.
- `BrFree(buffer)` frees any returned string, including `error_out`; null is
  accepted. Free exactly once. No Go pointers cross the interface.

Close/cancel return 1 for a valid handle, 0 otherwise. Errors from invalid
handles are returned, not panicked. Panics in an operation become generic
errors; runtime-fatal failures cannot be recovered. Input pointers must be
null or valid NUL-terminated UTF-8 strings for the whole call.

## Scope and limits

The prototype supports generic search and single-track direct FLAC/AAC
downloads. HLS/ffmpeg and `track_exists=skip` are rejected explicitly. The
CLI's skip branch does not return a path, which needs an explicit shared
contract before embedding that mode. Existing-file update/overwrite/error
settings otherwise retain their normal behavior. Artist queries, pagination,
batch downloads and UI progress are outside this prototype.

`library_core.go` composes the existing download functions in-process; it is
not yet a standalone importable Go package. The C exports are gated behind
the optional build tag. After validation, the service can be extracted into
an importable package without changing the external C interface.

## Host lifetime

The archive includes the Go runtime. Releasing all handles does **not** stop
that runtime. Do not `dlclose`/unmap a plugin bundle containing it: Go has no
supported runtime shutdown API. The Rust executable is safe because the
runtime lasts until process exit. VirtualDJ integration needs an explicit
process-lifetime image policy and a host teardown test before installation.
See [Go's unresolved unload issue](https://github.com/golang/go/issues/11100).

## Checks

```sh
CGO_ENABLED=0 go test ./...
CGO_ENABLED=1 go test -race -tags beetroot_library ./...
CGO_ENABLED=1 go vet -tags beetroot_library ./...
```

The tests cover in-flight HTTP cancellation, cancellation while queued,
single-use requests and a recoverable panic followed by another operation.
The Rust harness adds ABI ownership, invalid handles and client/request
lifetimes. Live search/download evidence belongs to the prototype README.
