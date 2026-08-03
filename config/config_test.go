package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig puts a TOML config in a temp dir and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "beetroot-dl.toml")
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

const minimalConfig = `
username = "u"
password = "p"
downloads_directory = "/tmp/downloads"
`

func TestParseAppliesDefaults(t *testing.T) {
	cfg, err := Parse(writeConfig(t, minimalConfig))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.Quality != "lossless" {
		t.Errorf("Quality = %q, want lossless", cfg.Quality)
	}
	if cfg.CoverSize != DefaultCoverSize {
		t.Errorf("CoverSize = %q, want %q", cfg.CoverSize, DefaultCoverSize)
	}
	if cfg.MaxDownloadWorkers != 15 {
		t.Errorf("MaxDownloadWorkers = %d, want 15", cfg.MaxDownloadWorkers)
	}
	if !cfg.FixTags {
		t.Error("FixTags = false, want true")
	}
	if cfg.TagMappings["flac"] == nil || cfg.TagMappings["m4a"] == nil {
		t.Error("default tag mappings were not populated")
	}
}

func TestParseRejectsInvalidValues(t *testing.T) {
	cases := map[string]struct{ body, wantErr string }{
		"missing credentials": {
			"downloads_directory = \"/tmp\"\n", "username or password",
		},
		"missing downloads directory": {
			"username = \"u\"\npassword = \"p\"\n", "downloads directory",
		},
		"invalid quality": {
			minimalConfig + "quality = \"flac\"\n", "invalid quality",
		},
		"invalid key system": {
			minimalConfig + "key_system = \"nonsense\"\n", "invalid key system",
		},
		"invalid track exists": {
			minimalConfig + "track_exists = \"clobber\"\n", "invalid track exists",
		},
		"track number padding too large": {
			minimalConfig + "track_number_padding = 11\n", "invalid track number padding",
		},
		"negative track number padding": {
			minimalConfig + "track_number_padding = -1\n", "invalid track number padding",
		},
		// A zero worker count is a hang, not a no-op: the semaphore channel
		// would have no capacity and every worker would block on it.
		"zero download workers": {
			minimalConfig + "max_download_workers = 0\n", "max_download_workers",
		},
		"negative global workers": {
			minimalConfig + "max_global_workers = -1\n", "max_global_workers",
		},
		"zero artists limit": {
			minimalConfig + "artists_limit = 0\n", "artists_limit",
		},
		"cover size without separator": {
			minimalConfig + "cover_size = \"1400\"\n", "cover_size",
		},
		"cover size not numeric": {
			minimalConfig + "cover_size = \"bigx1400\"\n", "cover_size",
		},
		"cover size too large": {
			minimalConfig + "cover_size = \"2000x2000\"\n", "cover_size",
		},
		"cover size zero": {
			minimalConfig + "cover_size = \"0x0\"\n", "cover_size",
		},
		// ParseTemplate leaves unknown placeholders verbatim, so a typo would
		// otherwise show up literally in every filename.
		"unknown track template placeholder": {
			minimalConfig + "track_file_template = \"{artist} - {name}\"\n", "{artist}",
		},
		"track placeholder in release template": {
			minimalConfig + "release_directory_template = \"{mix_name}\"\n", "{mix_name}",
		},
		"unknown artist template placeholder": {
			minimalConfig + "artist_directory_template = \"{nmae}\"\n", "{nmae}",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(writeConfig(t, tc.body))
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestParseAcceptsEverySupportedQuality(t *testing.T) {
	for _, q := range SupportedQualities {
		if q == "medium-hls" && !FFMPEGInstalled() {
			continue // legitimately rejected without ffmpeg
		}
		t.Run(q, func(t *testing.T) {
			if _, err := Parse(writeConfig(t, minimalConfig+"quality = \""+q+"\"\n")); err != nil {
				t.Errorf("Parse with quality %q: %v", q, err)
			}
		})
	}
}

// Every default template must validate against the keyword list, otherwise a
// fresh config would be rejected on first run.
func TestDefaultTemplatesAreValid(t *testing.T) {
	cfg, err := Parse(writeConfig(t, minimalConfig))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := validateTemplates(cfg); err != nil {
		t.Fatalf("default templates rejected: %v", err)
	}
}

func TestParseAcceptsValidTemplatesAndCoverSizes(t *testing.T) {
	bodies := []string{
		"cover_size = \"500x500\"\n",
		"cover_size = \"1400x1400\"\n",
		"track_file_template = \"{number}. {artists} - {name} ({mix_name}) [{key}]\"\n",
		"label_directory_template = \"{name} [{updated_date}]\"\n",
		"playlist_directory_template = \"{name} {bpm_range} {length}\"\n",
		// Text that is not a placeholder must pass through untouched.
		"artist_directory_template = \"just a name\"\n",
	}
	for _, body := range bodies {
		t.Run(strings.TrimSpace(body), func(t *testing.T) {
			if _, err := Parse(writeConfig(t, minimalConfig+body)); err != nil {
				t.Errorf("Parse: %v", err)
			}
		})
	}
}

// The config holds the account password in plaintext, so it must never be
// group- or world-readable.
func TestSaveUsesOwnerOnlyPermissions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "beetroot-dl.toml")
	cfg := &AppConfig{Username: "u", Password: "p", DownloadsDirectory: "/tmp"}
	if err := cfg.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("config permissions = %04o, want 0600", perm)
	}
}

// Save must also tighten an existing loose file, not leave it at 0644.
func TestSaveTightensExistingFilePermissions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "beetroot-dl.toml")
	if err := os.WriteFile(p, []byte("stale"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	cfg := &AppConfig{Username: "u", Password: "p", DownloadsDirectory: "/tmp"}
	if err := cfg.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("config permissions = %04o, want 0600", perm)
	}
}

// The first-run wizard saves a config built from Defaults(); anything it writes
// must load back cleanly. Saving a bare AppConfig used to serialise zero values
// that overrode Parse's defaults, so the very next run died on "invalid key
// system".
func TestSavedDefaultsRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "beetroot-dl.toml")
	cfg := Defaults()
	cfg.Username = "u"
	cfg.Password = "p"
	cfg.DownloadsDirectory = "/tmp/downloads"
	if err := cfg.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	parsed, err := Parse(p)
	if err != nil {
		t.Fatalf("Parse of saved config: %v", err)
	}
	if parsed.KeySystem != cfg.KeySystem {
		t.Errorf("KeySystem = %q, want %q", parsed.KeySystem, cfg.KeySystem)
	}
	if parsed.TrackExists != cfg.TrackExists {
		t.Errorf("TrackExists = %q, want %q", parsed.TrackExists, cfg.TrackExists)
	}
	if parsed.CoverSize != cfg.CoverSize {
		t.Errorf("CoverSize = %q, want %q", parsed.CoverSize, cfg.CoverSize)
	}
	if parsed.MaxDownloadWorkers != cfg.MaxDownloadWorkers {
		t.Errorf("MaxDownloadWorkers = %d, want %d", parsed.MaxDownloadWorkers, cfg.MaxDownloadWorkers)
	}
	if parsed.TrackFileTemplate != cfg.TrackFileTemplate {
		t.Errorf("TrackFileTemplate = %q, want %q", parsed.TrackFileTemplate, cfg.TrackFileTemplate)
	}
}
