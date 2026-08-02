package main

import (
	"os"
	"path"
	"testing"
)

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
