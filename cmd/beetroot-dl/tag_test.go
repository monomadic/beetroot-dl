package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/monomadic/beetroot-dl/config"
	"github.com/monomadic/beetroot-dl/internal/beatport"
	"go.senan.xyz/taglib"
)

// copyFixture copies a testdata file into a temp dir and returns the copy's
// path, so tests can tag it destructively.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	dst := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(dst, data, 0600); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	return dst
}

func testTrack() *beatport.Track {
	return &beatport.Track{
		ID:      1696999,
		Name:    "Strobe",
		MixName: "Original Mix",
		Number:  3,
		BPM:     128,
		ISRC:    "CA6D71600012",
		Genre:   beatport.Genre{Name: "Progressive House"},
		Release: beatport.Release{
			Name:       "Strobe EP",
			Date:       "2009-09-01",
			TrackCount: 4,
			Label:      beatport.Label{Name: "mau5trap"},
		},
	}
}

func testTagApp(t *testing.T) *application {
	t.Helper()
	return &application{
		config: &config.AppConfig{
			FixTags:     true,
			CoverSize:   config.DefaultCoverSize,
			TagMappings: config.DefaultTagMappings,
		},
	}
}

func TestTagTrackRoundTrip(t *testing.T) {
	for _, fixture := range []string{"silence.flac", "silence.m4a"} {
		t.Run(fixture, func(t *testing.T) {
			path := copyFixture(t, fixture)

			if err := testTagApp(t).tagTrack(path, testTrack(), ""); err != nil {
				t.Fatalf("tagTrack: %v", err)
			}

			tags, err := taglib.ReadTags(path)
			if err != nil {
				t.Fatalf("read tags back: %v", err)
			}

			want := map[string]string{
				"TITLE":       "Strobe (Original Mix)",
				"ALBUM":       "Strobe EP",
				"TRACKNUMBER": "3",
				"BPM":         "128",
				"ISRC":        "CA6D71600012",
				"DATE":        "2009-09-01",
				"LABEL":       "mau5trap",
			}
			for key, value := range want {
				got := tags[key]
				if len(got) != 1 || got[0] != value {
					t.Errorf("%s = %v, want [%s]", key, got, value)
				}
			}
		})
	}
}

// The Clear option must wipe tags that were already in the file — the old CGO
// path stripped everything before writing.
func TestTagTrackClearsExistingTags(t *testing.T) {
	path := copyFixture(t, "silence.flac")
	if err := taglib.WriteTags(path, map[string][]string{
		"COMMENT": {"pre-existing junk"},
	}, 0); err != nil {
		t.Fatalf("seed tags: %v", err)
	}

	if err := testTagApp(t).tagTrack(path, testTrack(), ""); err != nil {
		t.Fatalf("tagTrack: %v", err)
	}

	tags, err := taglib.ReadTags(path)
	if err != nil {
		t.Fatalf("read tags back: %v", err)
	}
	if _, ok := tags["COMMENT"]; ok {
		t.Errorf("COMMENT survived tagging, want it cleared; tags: %v", tags)
	}
}

func TestTagTrackEmbedsCover(t *testing.T) {
	for _, fixture := range []string{"silence.flac", "silence.m4a"} {
		t.Run(fixture, func(t *testing.T) {
			path := copyFixture(t, fixture)
			app := testTagApp(t)
			// The m4a path always embeds; FLAC only embeds when the cover was
			// resized away from the default, so force that for both.
			app.config.CoverSize = "500x500"

			if err := app.tagTrack(path, testTrack(), filepath.Join("testdata", "cover.jpg")); err != nil {
				t.Fatalf("tagTrack with cover: %v", err)
			}

			img, err := taglib.ReadImage(path)
			if err != nil {
				t.Fatalf("read image back: %v", err)
			}
			if len(img) == 0 {
				t.Error("no embedded image found after tagging")
			}
		})
	}
}

func TestTagTrackRawSuffixCompat(t *testing.T) {
	path := copyFixture(t, "silence.m4a")
	app := testTagApp(t)
	app.config.TagMappings = map[string]map[string]string{
		"m4a": {"track_key": "KEY_raw", "track_name": "TITLE"},
	}
	track := testTrack()

	if err := app.tagTrack(path, track, ""); err != nil {
		t.Fatalf("tagTrack: %v", err)
	}

	tags, err := taglib.ReadTags(path)
	if err != nil {
		t.Fatalf("read tags back: %v", err)
	}
	if got := tags["TITLE"]; len(got) != 1 || got[0] != "Strobe (Original Mix)" {
		t.Errorf("TITLE = %v, want [Strobe (Original Mix)]", got)
	}
}

func TestTagTrackRejectsUnknownExtension(t *testing.T) {
	if err := testTagApp(t).tagTrack("/tmp/whatever.mp3", testTrack(), ""); err == nil {
		t.Fatal("expected an error for an unsupported extension")
	}
}
