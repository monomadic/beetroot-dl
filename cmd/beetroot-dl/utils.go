package main

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

func (app *application) downloadFile(url string, destination string, pbPrefix string) error {
	out, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer out.Close()

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	if pbPrefix != "" {
		contentLength, _ := strconv.Atoi(resp.Header.Get("Content-Length"))
		bar := app.pbp.AddBar(int64(contentLength), ProgressBarOptions(pbPrefix)...)

		proxyReader := bar.ProxyReader(resp.Body)
		defer proxyReader.Close()

		_, err = io.Copy(out, proxyReader)
		if err != nil {
			return err
		}
	} else {
		_, err = io.Copy(out, resp.Body)
		if err != nil {
			return err
		}
	}

	return nil
}

func toMetaFunc(c *color.Color) func(string) string {
	return func(s string) string {
		return c.Sprint(s)
	}
}

func ProgressBarOptions(prefix string) []mpb.BarOption {
	red, green, blue := color.New(color.FgRed), color.New(color.FgGreen), color.New(color.FgBlue)

	options := []mpb.BarOption{
		mpb.BarFillerClearOnComplete(),
		mpb.PrependDecorators(
			decor.OnCompleteMeta(
				decor.OnComplete(
					decor.Meta(decor.Spinner([]string{"⣾ ", "⣽ ", "⣻ ", "⢿ ", "⡿ ", "⣟ ", "⣯ ", "⣷ "}), toMetaFunc(red)),
					"✓ ",
				),
				toMetaFunc(green),
			),
			decor.Name(prefix, decor.WCSyncSpaceR),
		),
		mpb.AppendDecorators(
			decor.OnComplete(decor.Meta(decor.Percentage(decor.WCSyncSpace), toMetaFunc(blue)), ""),
			decor.OnComplete(decor.Name(" |"), ""),
			decor.OnComplete(decor.Meta(
				decor.EwmaSpeed(decor.SizeB1000(0), "% .2f", 30, decor.WCSyncSpace), toMetaFunc(blue),
			), ""),
			decor.OnComplete(decor.Name(" | "), ""),
			decor.OnComplete(decor.Meta(decor.EwmaETA(decor.ET_STYLE_MMSS, 0, decor.WCSyncWidth), toMetaFunc(blue)), ""),
		),
	}
	return options
}

// isTerminal reports whether f is attached to a terminal, i.e. whether there
// is a human who could answer a prompt. Note that a ModeCharDevice check is
// NOT sufficient here: /dev/null is a character device too, so `< /dev/null`
// would look interactive and the prompt would read EOF instead of a person.
func isTerminal(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

// onExit runs just before the process leaves through an input-handling exit
// path (Ctrl-D at a prompt, an unreadable stdin). Those paths call os.Exit
// deep inside GetLine, so without this hook the run's results — the paths of
// everything already downloaded — would never be printed.
var onExit func()

func exitNow(code int) {
	if onExit != nil {
		onExit()
	}
	os.Exit(code)
}

func GetLine() string {
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		switch {
		case errors.Is(err, io.EOF):
			// Ctrl-D at a prompt is a normal way to quit.
			fmt.Fprintln(os.Stderr)
			exitNow(0)
		default:
			fmt.Fprintf(os.Stderr, "read input string: %v\n", err)
			exitNow(1)
		}
	}
	input = strings.TrimSuffix(input, "\n")
	input = strings.TrimSuffix(input, "\r")
	return input
}

// Pause blocks for a keypress. Only ever called in interactive mode — a
// non-interactive run must never hang waiting for a human (see Fatal).
func Pause() {
	fmt.Println("\nPress enter to exit")
	fmt.Scanln()
	os.Exit(1)
}

// Fatal reports an unrecoverable error and exits non-zero. In interactive mode
// it pauses first, so a double-clicked window does not vanish before the error
// can be read; otherwise it exits immediately, which is what a script needs.
func Fatal(interactive bool, format string, args ...any) {
	fmt.Fprintf(os.Stderr, "beetroot-dl: "+format+"\n", args...)
	if interactive {
		Pause()
	}
	os.Exit(1)
}

// LogError writes to stderr (not stdout) so that machine-readable output —
// the downloaded file paths — stays clean when piped, and counts the failure
// so the process can exit non-zero.
func (app *application) LogError(caller string, err error) {
	app.errCount.Add(1)
	message := fmt.Sprintf("%s: %s\n", caller, err.Error())
	fmt.Fprint(os.Stderr, message)

	if app.logFile != nil {
		app.logFile.WriteString(message)
	}
}

func (app *application) LogInfo(info string) {
	message := fmt.Sprintf("%s\n", info)
	fmt.Fprint(app.logWriter, message)
}

// LogVerbose reports intermediate download steps, only under --verbose. It
// goes to stderr for the same reason as LogError: stdout carries results.
func (app *application) LogVerbose(format string, args ...any) {
	if !app.verbose {
		return
	}
	fmt.Fprintf(os.Stderr, "  "+format+"\n", args...)
}

func (app *application) FatalError(caller string, err error) {
	app.LogError(caller, err)
	if app.interactive {
		Pause()
	}
	os.Exit(1)
}

func ExecutableDirFilePath(fileName string) (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %v", err)
	}
	execDir := filepath.Dir(execPath)
	filePathExec := filepath.Join(execDir, fileName)
	return filePathExec, nil
}

func WorkingDirFilePath(fileName string) (string, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %v", err)
	}

	filePathCurrent := filepath.Join(workingDir, fileName)
	return filePathCurrent, nil
}

// ConfigDir is where the config and credential cache live by default:
// $XDG_CONFIG_HOME/ (or ~/.config/) on every platform, not just Linux.
func ConfigDir() string {
	if xdgCfgHome, exists := os.LookupEnv("XDG_CONFIG_HOME"); exists && xdgCfgHome != "" {
		return xdgCfgHome
	}
	return path.Join(os.Getenv("HOME"), ".config")
}

// DefaultConfigPath is the canonical location, reported by --help and used
// when creating a config that does not exist yet.
func DefaultConfigPath() string {
	return path.Join(ConfigDir(), configFilename)
}

// FindConfigFile prefers ~/.config/beetroot-dl.toml. The working directory and
// the executable's own directory are still honoured when they already hold a
// config, so an existing portable/pinned-to-the-binary setup keeps working —
// but a *new* config is always created in ConfigDir(), never spilled next to
// the binary (which for a vendored/pinned binary may be a tracked repo).
func FindConfigFile() (string, bool, error) {
	filePathWorking, err := WorkingDirFilePath(configFilename)
	if err != nil {
		return "", false, err
	}
	filePathExec, err := ExecutableDirFilePath(configFilename)
	if err != nil {
		return "", false, err
	}

	for _, p := range []string{DefaultConfigPath(), filePathWorking, filePathExec} {
		if _, err := os.Stat(p); err == nil {
			return p, true, nil
		}
	}
	return DefaultConfigPath(), false, nil
}

// FindCacheFile keeps the session token beside the config by default, but
// follows an existing cache in the working/executable directory if the user
// already has one there.
func FindCacheFile() (string, bool, error) {
	defaultPath := path.Join(ConfigDir(), cacheFilename)

	filePathWorking, err := WorkingDirFilePath(cacheFilename)
	if err != nil {
		return "", false, err
	}
	filePathExec, err := ExecutableDirFilePath(cacheFilename)
	if err != nil {
		return "", false, err
	}

	for _, p := range []string{defaultPath, filePathWorking, filePathExec} {
		if _, err := os.Stat(p); err == nil {
			return p, true, nil
		}
	}
	return defaultPath, false, nil
}

func FindErrorLogFile() (string, bool, error) {
	var additionalDirs []string
	return findFile(errorFilename, additionalDirs)
}

func findFile(fileName string, additionalDirs []string) (string, bool, error) {
	configFilePaths := []string{}

	filePathWorking, err := WorkingDirFilePath(fileName)
	if err != nil {
		return "", false, err
	}
	configFilePaths = append(configFilePaths, filePathWorking)

	filePathExec, err := ExecutableDirFilePath(fileName)
	if err != nil {
		return "", false, err
	}
	configFilePaths = append(configFilePaths, filePathExec)

	for _, addadditionalDir := range additionalDirs {
		additionalFilePath := path.Join(addadditionalDir, fileName)
		configFilePaths = append(configFilePaths, additionalFilePath)
	}

	for _, configFilePath := range configFilePaths {
		_, err = os.Stat(configFilePath)
		if err == nil {
			return configFilePath, true, nil
		}
	}

	// last entry in list is default
	defaultFilePath := configFilePaths[len(configFilePaths)-1]
	return defaultFilePath, false, nil
}

func CreateDirectory(directory string) error {
	if _, err := os.Stat(directory); os.IsNotExist(err) {
		if err := os.MkdirAll(directory, 0760); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
	}
	return nil
}
