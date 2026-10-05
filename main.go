package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/monomadic/beetroot-dl/config"
	"github.com/monomadic/beetroot-dl/internal/beatport"

	"github.com/fatih/color"
	"github.com/vbauerster/mpb/v8"
)

const (
	configFilename = "beetroot-dl.toml"
	cacheFilename  = "beetroot-dl-credentials.json"
	errorFilename  = "beetroot-dl-err.log"
)

// version may be overridden at link time (-ldflags "-X main.version=...");
// otherwise the toolchain's own stamping is used — 'go install ...@v1.2.3'
// records the module version, and Go 1.24+ 'go build' in a checkout records
// the git tag/commit (with a +dirty suffix when the tree is modified).
var version = ""

func resolveVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

type application struct {
	config      *config.AppConfig
	logFile     *os.File
	logWriter   io.Writer
	ctx         context.Context
	wg          sync.WaitGroup
	downloadSem chan struct{}
	globalSem   chan struct{}
	pbp         *mpb.Progress

	verbose     bool
	interactive bool
	errCount    atomic.Int64

	// urls is read by the signal handler goroutine while the main loop
	// mutates it, so every access goes through urlsMutex.
	urls             []string
	urlsMutex        sync.Mutex
	activeFiles      map[string]struct{}
	activeFilesMutex sync.RWMutex

	// downloaded holds the final path of every track written this run, in
	// completion order. Printed to stdout on exit so callers can pipe it.
	downloaded      []string
	downloadedMutex sync.Mutex

	bp *beatport.Beatport
	// Optional library observer; called on the worker performing a transfer.
	progress func(done, total int64)
}

// addUrls queues URLs for the current pass.
func (app *application) addUrls(urls ...string) {
	app.urlsMutex.Lock()
	defer app.urlsMutex.Unlock()
	app.urls = append(app.urls, urls...)
}

// snapshotUrls returns a copy of the queue, safe to range over while the
// signal handler is looking at it.
func (app *application) snapshotUrls() []string {
	app.urlsMutex.Lock()
	defer app.urlsMutex.Unlock()
	return append([]string(nil), app.urls...)
}

func (app *application) clearUrls() {
	app.urlsMutex.Lock()
	defer app.urlsMutex.Unlock()
	app.urls = nil
}

func (app *application) hasUrls() bool {
	app.urlsMutex.Lock()
	defer app.urlsMutex.Unlock()
	return len(app.urls) > 0
}

func (app *application) recordDownload(path string) {
	app.downloadedMutex.Lock()
	defer app.downloadedMutex.Unlock()
	app.downloaded = append(app.downloaded, path)
}

const usageText = `beetroot-dl — download tracks from Beatport

USAGE
  beetroot-dl [flags] <url|query|file.txt>...
  beetroot-dl [flags] -i
  beetroot-dl [--config PATH] search --json <query>

By default it downloads what you asked for and exits. Pass -i for the
interactive prompt loop (the old default behaviour).

ARGUMENTS
  url        a beatport.com track, release, playlist, chart, label or artist URL
  file.txt   a file of URLs, one per line (any argument ending in .txt)
  query      anything else is treated as a search query; needs -i to pick a
             result, since choosing one requires a prompt

FLAGS
  -i, --interactive   keep prompting for more URLs after finishing
  -v, --verbose       report each step (fetch, download, tag) to stderr
  -q, --quiet         suppress progress bars
      --config PATH   use this config file instead of the default
      --completions SHELL
                      print a completion script for SHELL (bash or zsh) and
                      exit; see COMPLETIONS below
      --version       print version and exit
  -h, --help          show this help

OUTPUT
  The path of each downloaded track is printed to stdout, one per line.
  The search command prints one flat JSON object per track on stdout.
  Progress bars, errors and verbose steps go to stderr, so stdout stays
  usable in a pipe:

    beetroot-dl "https://www.beatport.com/track/strobe/1696999" | while read -r f; do
      echo "got: $f"
    done

EXIT STATUS
  0  everything requested was downloaded (or already present)
  1  at least one item failed
  2  usage error — no URL given and not running interactively

CONFIG
  %s
  Created interactively on first run if missing. A config already sitting in
  the working directory or beside the binary is still honoured.

COMPLETIONS
  zsh:   beetroot-dl --completions zsh > "${fpath[1]}/_beetroot-dl"
  bash:  beetroot-dl --completions bash > ~/.local/share/bash-completion/completions/beetroot-dl
  Then start a new shell.
`

func usage() {
	fmt.Fprintf(os.Stderr, usageText, DefaultConfigPath())
}

func main() {
	// Flags are parsed BEFORE any config loading or network access, so --help
	// and --version always work — even with no config, no credentials and no
	// connection. (Upstream parsed them after Setup(), so --help on a fresh
	// machine would first interrogate you for a username and password.)
	var (
		interactive bool
		verbose     bool
		quiet       bool
		showVersion bool
		configPath  string
		completions string
	)
	flag.Usage = usage
	flag.BoolVar(&interactive, "i", false, "")
	flag.BoolVar(&interactive, "interactive", false, "")
	flag.BoolVar(&verbose, "v", false, "")
	flag.BoolVar(&verbose, "verbose", false, "")
	flag.BoolVar(&quiet, "q", false, "")
	flag.BoolVar(&quiet, "quiet", false, "")
	flag.BoolVar(&showVersion, "version", false, "")
	flag.StringVar(&configPath, "config", "", "")
	flag.StringVar(&completions, "completions", "", "")
	flag.Parse()

	if showVersion {
		fmt.Println(resolveVersion())
		return
	}

	if completions != "" {
		runCompletions(completions)
	}
	// Search is a separate, non-interactive command for consumers such as
	// VirtualDJ. Keep the normal downloader's stdout contract unchanged.
	if flag.NArg() > 0 && flag.Arg(0) == "search" {
		runSearchCommand(flag.Args()[1:], configPath)
		return
	}

	inputArgs := flag.Args()
	if len(inputArgs) == 0 && !interactive {
		usage()
		os.Exit(2)
	}

	cfg, cachePath, err := Setup(configPath, interactive)
	if err != nil {
		Fatal(interactive, "%s", err.Error())
	}

	if quiet {
		cfg.ShowProgress = false
	}

	ctx, cancel := context.WithCancel(context.Background())

	app := &application{
		config:      cfg,
		downloadSem: make(chan struct{}, cfg.MaxDownloadWorkers),
		globalSem:   make(chan struct{}, cfg.MaxGlobalWorkers),
		ctx:         ctx,
		logWriter:   os.Stderr,
		verbose:     verbose,
		interactive: interactive,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		<-sigCh

		if app.hasUrls() {
			app.LogInfo("Shutdown signal received. Waiting for download workers to finish")
			cancel()

			<-sigCh
		}

		// 130 = terminated by SIGINT, the shell convention.
		app.printResults()
		os.Exit(130)
	}()

	if cfg.WriteErrorLog {
		logFilePath, _, err := FindErrorLogFile()
		if err != nil {
			Fatal(interactive, "%s", err.Error())
		}
		f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			Fatal(interactive, "open error log: %s", err.Error())
		}
		app.logFile = f
		defer f.Close()
	}

	auth := beatport.NewAuth(cfg.Username, cfg.Password, cachePath)
	bp, err := beatport.New(cfg.Proxy, auth)
	if err != nil {
		Fatal(interactive, "%s", err.Error())
	}

	app.LogVerbose("authenticating as %s", cfg.Username)
	if err := auth.LoadCache(); err != nil {
		app.LogVerbose("no cached session (%v), logging in", err)
		if err := auth.Init(bp); err != nil {
			app.FatalError("beatport", err)
		}
	} else {
		app.LogVerbose("using cached session from %s", cachePath)
	}

	app.bp = bp

	for _, arg := range inputArgs {
		if strings.HasSuffix(arg, ".txt") {
			app.parseTextFile(arg)
		} else {
			app.addUrls(arg)
		}
	}

	for {
		if !app.hasUrls() {
			// Quitting the prompt with Ctrl-D (io.EOF) is a normal way to
			// finish; anything else is a real stdin failure worth reporting.
			// Either way the run's results are printed on the way out.
			if err := app.mainPrompt(); err != nil {
				if !errors.Is(err, io.EOF) {
					app.LogError("read input", err)
				}
				break
			}
		}

		urls := app.snapshotUrls()

		app.pbp = mpb.New(mpb.WithAutoRefresh(), mpb.WithOutput(color.Error))
		app.activeFiles = make(map[string]struct{}, len(urls))

		for _, url := range urls {
			app.globalWorker(func() {
				app.handleUrl(url)
			})
		}

		app.wg.Wait()
		app.pbp.Shutdown()

		if !interactive || ctx.Err() != nil {
			break
		}

		app.clearUrls()
	}

	app.printResults()
	if app.errCount.Load() > 0 {
		os.Exit(1)
	}
}

// printResults writes every downloaded path to stdout — the machine-readable
// result of the run — and a human summary to stderr.
func (app *application) printResults() {
	app.downloadedMutex.Lock()
	defer app.downloadedMutex.Unlock()

	for _, p := range app.downloaded {
		fmt.Println(p)
	}

	failed := app.errCount.Load()
	switch {
	case len(app.downloaded) == 0 && failed == 0:
		fmt.Fprintln(os.Stderr, "nothing downloaded")
	case failed == 0:
		fmt.Fprintf(os.Stderr, "%d track(s) downloaded\n", len(app.downloaded))
	default:
		fmt.Fprintf(os.Stderr, "%d track(s) downloaded, %d error(s)\n", len(app.downloaded), failed)
	}
}
