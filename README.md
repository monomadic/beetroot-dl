# beetroot-dl

Beatport downloader (FLAC, AAC) — a fork of
[unspok3n/beatportdl](https://github.com/unspok3n/beatportdl) reworked:

- **pure go** drops the dependencies on CGO, zig, Make, etc
- now just a standard go package: run `go install`, `go build`, etc
- behaves like a normal unix command line tool _(see 'Behavior')_
- config file now follows convention instead of forcibly being
    next to the binary
- full auto completion for zsh/bash
- many bug fixes and structural improvements

```shell
beetroot-dl https://www.beatport.com/track/strobe/1696999
```

## What changed from upstream

**Behaviour**

| | upstream `beatportdl` | `beetroot-dl` |
|---|---|---|
| after a download | drops into an interactive prompt loop | exits |
| interactive loop | always on | opt-in with `-i` |
| `--help` | none | full usage screen |
| flag parsing | after config load + login | before anything else |
| exit status | always `0` | `0` ok, `1` failure, `2` usage, `130` interrupted |
| downloaded paths | never reported | printed to stdout, one per line |
| progress/errors/prompts | mixed into stdout | all on stderr |
| config format | YAML | TOML |
| config location | next to the binary | `~/.config/beetroot-dl.toml` |
| `XDG_CONFIG_HOME` | Linux only | all platforms |
| missing config, no tty | blocks forever on a prompt | fails with an explanation |
| fatal error | "Press enter to exit" (blocks) | message on stderr, exits |

**The point of most of it:** a run is now scriptable. stdout carries only
results, so the output pipes; the exit status says whether it worked; and
nothing waits on a human unless you asked for a prompt.

```shell
# cue every track you just bought, without guessing what was written where
beetroot-dl "$URL" | while read -r f; do process "$f"; done || echo "something failed"
```

**Fixes**

- **`--help` works on a fresh machine.** Flags were parsed *after* `Setup()`,
  so asking for help on a box with no config would first interrogate you for
  a username and password.
- **Piped/cron runs no longer hang.** With no config and no terminal, the
  first-run prompt would block forever; it now explains what to run instead.
- **Panics in download workers are no longer swallowed.** They printed to
  stdout and were discarded, so a crashed download still exited `0`. They are
  now reported on stderr and counted.
- **Results survive Ctrl-D.** Quitting a prompt called `os.Exit` deep inside
  the input reader, so paths already downloaded were never printed.
- **`Logging in` / `Refreshing token` no longer pollute stdout.**

**Additions**

- `-v` / `--verbose` — report each step (auth, resolve, fetch metadata,
  target directory, write, tag) on stderr.
- `-q` / `--quiet` — suppress progress bars.
- `--config PATH` — use a specific config file.
- `--completions bash|zsh` — print a shell completion script (install
  instructions under [Setup](#setup)).
- `--version`.

**Breaking**

- The binary is `beetroot-dl`, not `beatportdl`.
- Config is TOML at `~/.config/beetroot-dl.toml`; port an existing
  `beatportdl-config.yml` by hand (`key: value` → `key = "value"`).
- Upstream's `-q` meant "quit after finishing" — that is the default now, and
  `-q` means `--quiet`.

*Requires an active [Beatport](https://stream.beatport.com/) or [Beatsource](https://stream.beatsource.com/) streaming plan.*

## Highlights over upstream

An optional [static C library prototype](docs/C_LIBRARY.md) lets Rust/C hosts
reuse the downloader in-process. It requires CGO for the archive build;
the normal CLI retains its pure-Go build.

- Scriptable by default: downloads what you asked for and exits; stdout
  carries only the downloaded file paths, so output pipes cleanly.
- Real exit codes (`0` ok, `1` failure, `2` usage, `130` interrupted).
- `--help`, `--version`, `-v`, `-q`, `--config` — and they work before any
  config or login.
- TOML config at `~/.config/beetroot-dl.toml` instead of YAML next to the
  binary; `XDG_CONFIG_HOME` respected on all platforms.
- Never blocks on a prompt in a pipe or cron job.
- Pure Go — no CGO, so `go install github.com/monomadic/beetroot-dl@latest`
  works on a machine with nothing but Go, and cross-compiling needs no C
  toolchain (upstream requires taglib headers and zig to build).

*(Short for now — this list grows as [TASKS.md](TASKS.md) gets worked
through.)*

Setup
---
1. [Download](https://github.com/monomadic/beetroot-dl/releases/) or [build](#building) beetroot-dl.

     *Compiled binaries for Windows, macOS (amd64, arm64) and Linux (amd64, arm64) are available on the [Releases](https://github.com/monomadic/beetroot-dl/releases) page.* \
     *Don't forget to set the execute permission on unix systems, e.g., chmod +x beetroot-dl-darwin-arm64*

2. Run beetroot-dl with `-i` (e.g. `./beetroot-dl-darwin-arm64 -i`), then specify the:
   - Beatport username
   - Beatport password
   - Downloads directory
   - Audio quality

3. OPTIONAL: Install shell completions — the binary carries its own scripts:

   ```shell
   # zsh — write into any directory on your $fpath, then restart the shell
   beetroot-dl --completions zsh > "${fpath[1]}/_beetroot-dl"

   # bash
   beetroot-dl --completions bash > ~/.local/share/bash-completion/completions/beetroot-dl
   ```

4. OPTIONAL: Customize the config file at `~/.config/beetroot-dl.toml`
   (or `$XDG_CONFIG_HOME/beetroot-dl.toml`), created on first run. It is TOML,
   so values are `key = "value"`. You can put the following options into it:

---
| Option                        | Default Value                             | Type       | Description                                                                                                                                                                               |
|-------------------------------|-------------------------------------------|------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `username`                    |                                           | String     | Beatport username                                                                                                                                                                         |
| `password`                    |                                           | String     | Beatport password                                                                                                                                                                         |
| `quality`                     | lossless                                  | String     | Download quality *(medium-hls, medium, high, lossless)*                                                                                                                                   |
| `show_progress`               | true                                      | Boolean    | Enable progress bars                                                                                                                                                                      |
| `write_error_log`             | false                                     | Boolean    | Write errors to `error.log`                                                                                                                                                               |
| `max_download_workers`        | 15                                        | Integer    | Concurrent download jobs limit                                                                                                                                                            |
| `max_global_workers`          | 15                                        | Integer    | Concurrent global jobs limit                                                                                                                                                              |
| `downloads_directory`         |                                           | String     | Location for the downloads directory                                                                                                                                                      |
| `sort_by_context`             | false                                     | Boolean    | Create a directory for each release, playlist, chart, label, or artist                                                                                                                    |
| `sort_by_label`               | false                                     | Boolean    | Use label names as parent directories for releases (requires `sort_by_context`)                                                                                                           |
| `force_release_directories`   | false                                     | Boolean    | Create release directories inside chart and playlist folders (requires `sort_by_context`)                                                                                                 |
| `track_exists`                | update                                    | String     | Behavior when track file already exists                                                                                                                                                   |
| `track_number_padding`        | 2                                         | Integer    | Track number padding for filenames and tag mappings (when using `track_number_with_padding` or `release_track_count_with_padding`)<br/> Set to 0 for dynamic padding based on track count |
| `cover_size`                  | 1400x1400                                 | String     | Cover art size for `keep_cover` and track metadata (if `fix_tags` is enabled)  *[max: 1400x1400]*                                                                                         |
| `keep_cover`                  | false                                     | Boolean    | Download cover art file (cover.jpg) to the context directory (requires `sort_by_context`)                                                                                                 |
| `fix_tags`                    | true                                      | Boolean    | Enable tag writing capabilities                                                                                                                                                           |
| `tag_mappings`                | *Listed below*                            | String Map | Custom tag mappings                                                                                                                                                                       |
| `track_file_template`         | {number}. {artists} - {name} ({mix_name}) | String     | Track filename template                                                                                                                                                                   |
| `release_directory_template`  | [{catalog_number}] {artists} - {name}     | String     | Release directory template                                                                                                                                                                |
| `playlist_directory_template` | {name} [{created_date}]                   | String     | Playlist directory template                                                                                                                                                               |
| `chart_directory_template`    | {name} [{published_date}]                 | String     | Chart directory template                                                                                                                                                                  |
| `label_directory_template`    | {name} [{updated_date}]                   | String     | Label directory template                                                                                                                                                                  |
| `artist_directory_template`   | {name}                                    | String     | Artist directory template                                                                                                                                                                 |
| `whitespace_character`        |                                           | String     | Whitespace character for track filenames and release directories                                                                                                                          |
| `artists_limit`               | 3                                         | Integer    | Maximum number of artists allowed before replacing with `artists_short_form` (affects directories, filenames, and search results)                                                         |
| `artists_short_form`          | VA                                        | String     | Custom string to represent "Various Artists"                                                                                                                                              |
| `key_system`                  | standard-short                            | String     | Music key system used in filenames and tags                                                                                                                                               |
| `proxy`                       |                                           | String     | Proxy URL                                                                                                                                                                                 |

If the Beatport credentials are correct, you should also see the file `beetroot-dl-credentials.json` appear beside the config.
*If you accidentally entered an incorrect password and got an error, you can always manually edit the config file*

Download quality options, per Beatport/Beatsource subscription type:

| Option       | Description                                                                                                  | Requires at least              | Notes                                                                   |
|--------------|--------------------------------------------------------------------------------------------------------------|--------------------------------|-------------------------------------------------------------------------|
| `medium-hls` | 128 kbps AAC through `/stream` endpoint (IMPORTANT: requires [ffmpeg](https://www.ffmpeg.org/download.html)) | Essential / Beatsource         | Same as `medium` on Advanced but uses a slightly slower download method |
| `medium`     | 128 kbps AAC                                                                                                 | Advanced / Beatsource Pro+     |                                                                         |
| `high`       | 256 kbps AAC                                                                                                 | Professional / Beatsource Pro+ |                                                                         |
| `lossless`   | 44.1 kHz FLAC                                                                                                | Professional / Beatsource Pro+ |                                                                         |

Available `track_exists` options:
* `error` Log error and skip
* `skip` Skip silently
* `overwrite` Re-download
* `update` Update tags

Available template keywords for filenames and directories (`*_template`):
* Track: `id`,`name`,`mix_name`,`slug`,`artists`,`remixers`,`number`,`length`,`key`,`bpm`,`genre`,`subgenre`,`genre_with_subgenre`,`subgenre_or_genre`,`isrc`,`label`
* Release: `id`,`name`,`slug`,`artists`,`remixers`,`date`,`year`,`track_count`,`bpm_range`,`catalog_number`,`upc`,`label`
* Playlist: `id`,`name`,`first_genre`,`track_count`,`bpm_range`,`length`,`created_date`,`updated_date`
* Chart: `id`,`name`,`slug`,`first_genre`,`track_count`,`creator`,`created_date`,`published_date`,`updated_date`
* Artist: `id`, `name`, `slug`
* Label: `id`, `name`, `slug`, `created_date`, `updated_date`

Default `tag_mappings` config:
```yaml
tag_mappings:
   flac:
      track_name: "TITLE"
      track_artists: "ARTIST"
      track_number: "TRACKNUMBER"
      track_subgenre_or_genre: "GENRE"
      track_key: "KEY"
      track_bpm: "BPM"
      track_isrc: "ISRC"
   
      release_name: "ALBUM"
      release_artists: "ALBUMARTIST"
      release_date: "DATE"
      release_track_count: "TOTALTRACKS"
      release_catalog_number: "CATALOGNUMBER"
      release_label: "LABEL"
   m4a:
      track_name: "TITLE"
      track_artists: "ARTIST"
      track_number: "TRACKNUMBER"
      track_genre: "GENRE"
      track_key: "KEY"
      track_bpm: "BPM"
      track_isrc: "ISRC"
   
      release_name: "ALBUM"
      release_artists: "ALBUMARTIST"
      release_date: "DATE"
      release_track_count: "TOTALTRACKS"
      release_catalog_number: "CATALOGNUMBER"
      release_label: "LABEL"
```

As you can see, each key here represents a predefined value from either a release or a track that you can use to customize what is written to which tags. When you add an entry in the mappings for any format (for e.g., `flac`), only the tags that you specify will be written.

All tags by default are converted to uppercase, but since some M4A players might not recognize it, you can write the tag in lowercase and add the `_raw` suffix to bypass the conversion. *(This applies to M4A tags only)*

For e.g., Traktor doesn't recognize the track key tag in uppercase, so you have to add:
```yaml
tag_mappings:
   m4a:
      track_key: "initialkey_raw"
```

Available `tag_mappings` keys: `track_id`,`track_url`,`track_name`,`track_artists`,`track_artists_limited`,`track_remixers`,`track_remixers_limited`,`track_number`,`track_number_with_padding`,`track_number_with_total`,`track_genre`,`track_subgenre`,`track_genre_with_subgenre`,`track_subgenre_or_genre`,`track_key`,`track_bpm`,`track_isrc`,`release_id`,`release_url`,`release_name`,`release_artists`,`release_artists_limited`,`release_remixers`,`release_remixers_limited`,`release_date`,`release_year`,`release_track_count`,`release_track_count_with_padding`,`release_catalog_number`,`release_upc`,`release_label`,`release_label_url`

Available `key_system` options:

| System           | Example           |
|------------------|-------------------|
| `standard`       | Eb Minor, F Major |
| `standard-short` | Ebm, F            |
| `openkey`        | 7m, 12d           |
| `camelot`        | 2A, 7B            |

Proxy URL format example: `http://username:password@127.0.0.1:8080`

Usage
---

Give it a URL; it downloads and exits.
```shell
beetroot-dl https://www.beatport.com/track/strobe/1696999
```
Multiple URLs, or text files of URLs (one per line), both work:
```shell
beetroot-dl https://www.beatport.com/track/strobe/1696999 https://www.beatport.com/track/move-for-me/591753
beetroot-dl file.txt file2.txt
```

The path of each downloaded track goes to stdout, one per line, so a run
composes with other tools:
```shell
beetroot-dl https://www.beatport.com/track/strobe/1696999 | while read -r f; do
  echo "got: $f"
done
```
Progress bars, errors and `-v` step reporting all go to stderr, and the exit
status says whether it worked — so `beetroot-dl ... || echo failed` is
meaningful.

For the interactive prompt loop (search queries, label/artist filters), pass
`-i`:
```shell
beetroot-dl -i
Enter url or search query:
```
Search returns beatport results; include an `@beatsource` tag in the query to
search beatsource instead. A search query needs `-i`, since picking a result
requires a prompt.

For tools that need search results without a prompt, use
`beetroot-dl search --json "query"`. It prints one track per line as JSON,
including its ID, title, artists, tempo, key, cover URL and Beatport URL.
No match prints nothing and exits successfully; authentication or network
errors exit nonzero with a message on stderr. A `--config PATH` option goes
before `search` if you use a non-default config.

Run `beetroot-dl --help` for the full flag list.

URL types that are currently supported: **Tracks, Releases, Playlists, Charts, Labels, Artists**

Building
---

Pure Go — no C toolchain, headers, system libraries, Makefile, or environment
variables needed. Tagging uses [TagLib](https://taglib.org/) compiled to
WebAssembly ([go.senan.xyz/taglib](https://github.com/sentriz/go-taglib)),
embedded in the binary and run in-process.

Install straight from source:

```shell
go install github.com/monomadic/beetroot-dl@latest
```

Or from a checkout — a bare `go install`, or:

```shell
go build
```

Cross-compiling is standard Go — `GOOS=... GOARCH=... go build`, nothing
else. `--version` reports the module version (or git tag/commit) stamped by
the Go toolchain itself; no build flags required.

`ffmpeg` is required at runtime only for the `medium-hls` quality setting.
