package beatport

import (
	"errors"
	"testing"
)

// ParseUrl does not touch the receiver, so a zero-value client is enough.
var linkParser = &Beatport{}

func TestParseUrlTypes(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantType LinkType
		wantID   int64
	}{
		{"track", "https://www.beatport.com/track/strobe/1696999", TrackLink, 1696999},
		{"release", "https://www.beatport.com/release/for-lack-of-a-better-name/91231", ReleaseLink, 91231},
		{"chart", "https://www.beatport.com/chart/best-of-2024/123456", ChartLink, 123456},
		{"label", "https://www.beatport.com/label/mau5trap/12345", LabelLink, 12345},
		{"artist", "https://www.beatport.com/artist/deadmau5/1234", ArtistLink, 1234},

		// "playlist" in the store namespace is a chart; playlists live under
		// /library or /playlists.
		{"store playlist is a chart", "https://www.beatport.com/playlist/foo/999", ChartLink, 999},
		{"playlists", "https://www.beatport.com/playlists/my-set/4242", PlaylistLink, 4242},
		// Library links carry no slug: the id is the third segment.
		{"library playlists", "https://www.beatport.com/library/playlists/777", PlaylistLink, 777},
		{"library playlist singular", "https://www.beatport.com/library/playlist/778", PlaylistLink, 778},

		// API-shaped URLs put the id one segment earlier.
		{"api tracks", "https://api.beatport.com/v4/catalog/tracks/1696999", TrackLink, 1696999},
		{"api releases", "https://api.beatport.com/v4/catalog/releases/91231", ReleaseLink, 91231},

		// A two-letter leading segment is a locale and is skipped.
		{"locale prefix", "https://www.beatport.com/en/track/strobe/1696999", TrackLink, 1696999},
		{"locale and catalog prefix", "https://www.beatport.com/de/catalog/release/x/91231", ReleaseLink, 91231},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link, err := linkParser.ParseUrl(tt.url)
			if err != nil {
				t.Fatalf("ParseUrl(%q) returned error: %v", tt.url, err)
			}
			if link.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", link.Type, tt.wantType)
			}
			if link.ID != tt.wantID {
				t.Errorf("ID = %d, want %d", link.ID, tt.wantID)
			}
			if link.Original != tt.url {
				t.Errorf("Original = %q, want %q", link.Original, tt.url)
			}
		})
	}
}

func TestParseUrlKeepsQueryParams(t *testing.T) {
	const raw = "https://www.beatport.com/label/mau5trap/12345?genre=1&publish_date=2024-01-01"
	link, err := linkParser.ParseUrl(raw)
	if err != nil {
		t.Fatalf("ParseUrl: %v", err)
	}
	want := "genre=1&publish_date=2024-01-01"
	if link.Params != want {
		t.Errorf("Params = %q, want %q", link.Params, want)
	}
}

func TestParseUrlRejects(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"foreign host", "https://example.com/track/strobe/1696999"},
		{"soundcloud", "https://soundcloud.com/deadmau5/strobe"},
		{"empty path", "https://www.beatport.com/"},
		{"unknown type", "https://www.beatport.com/genre/techno/6"},
		{"missing id segment", "https://www.beatport.com/track/strobe"},
		{"non-numeric id", "https://www.beatport.com/track/strobe/not-a-number"},
		{"library with no sub-type", "https://www.beatport.com/library"},
		{"library with unknown sub-type", "https://www.beatport.com/library/downloads/1"},
		{"locale-stripped library with no sub-type", "https://www.beatport.com/en/library"},
		{"scheme only", "https://"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link, err := linkParser.ParseUrl(tt.url)
			if err == nil {
				t.Fatalf("ParseUrl(%q) = %+v, want error", tt.url, link)
			}
		})
	}
}

// A malformed URL should come back as an error from url.Parse, not a panic.
func TestParseUrlMalformed(t *testing.T) {
	if _, err := linkParser.ParseUrl("://not a url"); err == nil {
		t.Fatal("ParseUrl(malformed) = nil error, want error")
	}
}

func TestParseUrlInvalidUrlIsSentinel(t *testing.T) {
	_, err := linkParser.ParseUrl("https://example.com/track/strobe/1")
	if !errors.Is(err, ErrInvalidUrl) {
		t.Fatalf("err = %v, want ErrInvalidUrl", err)
	}
}
