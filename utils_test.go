package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"testing"
)

// readLines drives GetLine against a canned stdin.
func readLines(t *testing.T, input string, n int) ([]string, error) {
	t.Helper()
	original := stdin
	t.Cleanup(func() { stdin = original })
	stdin = bufio.NewReader(strings.NewReader(input))

	var got []string
	for i := 0; i < n; i++ {
		line, err := GetLine()
		if err != nil {
			return got, err
		}
		got = append(got, line)
	}
	return got, nil
}

// GetLine used to call os.Exit on Ctrl-D from deep inside the prompt, which is
// why none of this could be tested before.
func TestGetLine(t *testing.T) {
	t.Run("reads successive lines", func(t *testing.T) {
		got, err := readLines(t, "first\nsecond\n", 2)
		if err != nil {
			t.Fatalf("GetLine: %v", err)
		}
		if len(got) != 2 || got[0] != "first" || got[1] != "second" {
			t.Fatalf("lines = %q, want [first second]", got)
		}
	})

	t.Run("strips a trailing CR", func(t *testing.T) {
		got, err := readLines(t, "windows\r\n", 1)
		if err != nil {
			t.Fatalf("GetLine: %v", err)
		}
		if got[0] != "windows" {
			t.Fatalf("line = %q, want %q", got[0], "windows")
		}
	})

	t.Run("returns io.EOF when input closes", func(t *testing.T) {
		_, err := readLines(t, "", 1)
		if !errors.Is(err, io.EOF) {
			t.Fatalf("err = %v, want io.EOF", err)
		}
	})

	t.Run("returns a final line with no newline", func(t *testing.T) {
		got, err := readLines(t, "no trailing newline", 1)
		if err != nil {
			t.Fatalf("GetLine: %v", err)
		}
		if got[0] != "no trailing newline" {
			t.Fatalf("line = %q", got[0])
		}
	})

	t.Run("signals EOF after the last unterminated line", func(t *testing.T) {
		got, err := readLines(t, "only\n", 2)
		if !errors.Is(err, io.EOF) {
			t.Fatalf("err = %v, want io.EOF after the input is exhausted", err)
		}
		if len(got) != 1 || got[0] != "only" {
			t.Fatalf("lines = %q, want [only]", got)
		}
	})
}

// The config and the credential cache both live directly in the config dir —
// $XDG_CONFIG_HOME if set, else ~/.config — on every platform. There is no
// per-app subdirectory, and no separate state dir: the file names are already
// distinctive, and keeping the pair together means one place to look.

func TestFindConfigFile(t *testing.T) {
	t.Run("defaults to ~/.config when XDG_CONFIG_HOME is unset", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")

		configFilePath, _, gotErr := FindConfigFile()
		if gotErr != nil {
			t.Fatalf("FindConfigFile() failed: %v", gotErr)
		}

		expectedPath := path.Join(os.Getenv("HOME"), ".config", configFilename)
		if expectedPath != configFilePath {
			t.Errorf("Paths do not match: %s != %s", expectedPath, configFilePath)
		}
	})

	t.Run("honours XDG_CONFIG_HOME when set", func(t *testing.T) {
		xdgConfigHome := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)

		configFilePath, _, gotErr := FindConfigFile()
		if gotErr != nil {
			t.Fatalf("FindConfigFile() failed: %v", gotErr)
		}

		expectedPath := path.Join(xdgConfigHome, configFilename)
		if expectedPath != configFilePath {
			t.Errorf("Paths do not match: %s != %s", expectedPath, configFilePath)
		}
	})

	t.Run("reports whether the file exists", func(t *testing.T) {
		xdgConfigHome := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)

		_, exists, err := FindConfigFile()
		if err != nil {
			t.Fatalf("FindConfigFile() failed: %v", err)
		}
		if exists {
			t.Errorf("exists = true for a config that was never created")
		}

		if err := os.WriteFile(path.Join(xdgConfigHome, configFilename), []byte("username = \"x\"\n"), 0600); err != nil {
			t.Fatal(err)
		}

		_, exists, err = FindConfigFile()
		if err != nil {
			t.Fatalf("FindConfigFile() failed: %v", err)
		}
		if !exists {
			t.Errorf("exists = false for a config that is present")
		}
	})
}

func TestFindCacheFile(t *testing.T) {
	t.Run("defaults to ~/.config when XDG_CONFIG_HOME is unset", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")

		cacheFilePath, _, gotErr := FindCacheFile()
		if gotErr != nil {
			t.Fatalf("FindCacheFile() failed: %v", gotErr)
		}

		expectedPath := path.Join(os.Getenv("HOME"), ".config", cacheFilename)
		if expectedPath != cacheFilePath {
			t.Errorf("Paths do not match: %s != %s", expectedPath, cacheFilePath)
		}
	})

	t.Run("sits beside the config when XDG_CONFIG_HOME is set", func(t *testing.T) {
		xdgConfigHome := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)

		cacheFilePath, _, gotErr := FindCacheFile()
		if gotErr != nil {
			t.Fatalf("FindCacheFile() failed: %v", gotErr)
		}

		expectedPath := path.Join(xdgConfigHome, cacheFilename)
		if expectedPath != cacheFilePath {
			t.Errorf("Paths do not match: %s != %s", expectedPath, cacheFilePath)
		}
	})
}
