package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"monomadic/beetroot-dl/config"
	"monomadic/beetroot-dl/internal/beatport"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/fatih/color"
	"github.com/vbauerster/mpb/v8"
)

const (
	configFilename = "beetroot-dl.toml"
	cacheFilename  = "beetroot-dl-credentials.json"
	errorFilename  = "beetroot-dl-err.log"
)

var version = "dev"

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

	urls             []string
	activeFiles      map[string]struct{}
	activeFilesMutex sync.RWMutex

	// downloaded holds the final path of every track written this run, in
	// completion order. Printed to stdout on exit so callers can pipe it.
	downloaded      []string
	downloadedMutex sync.Mutex

	bp *beatport.Beatport
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
		fmt.Println(version)
		return
	}

	if completions != "" {
		runCompletions(completions)
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

		if len(app.urls) > 0 {
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
	bp := beatport.New(cfg.Proxy, auth)

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

	// Quitting from a prompt (Ctrl-D) exits from inside GetLine; make sure the
	// run's results still get printed on the way out.
	onExit = app.printResults

	for _, arg := range inputArgs {
		if strings.HasSuffix(arg, ".txt") {
			app.parseTextFile(arg)
		} else {
			app.urls = append(app.urls, arg)
		}
	}

	for {
		if len(app.urls) == 0 {
			app.mainPrompt()
		}

		app.pbp = mpb.New(mpb.WithAutoRefresh(), mpb.WithOutput(color.Error))
		app.activeFiles = make(map[string]struct{}, len(app.urls))

		for _, url := range app.urls {
			app.globalWorker(func() {
				app.handleUrl(url)
			})
		}

		app.wg.Wait()
		app.pbp.Shutdown()

		if !interactive || ctx.Err() != nil {
			break
		}

		app.urls = []string{}
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
