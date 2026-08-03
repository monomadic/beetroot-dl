package main

import (
	"testing"

	"github.com/monomadic/beetroot-dl/internal/beatport"
)

func TestNormaliseDateBounds(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantFrom string
		wantTo   string
	}{
		{"empty stays empty", "", "", ""},
		{"year expands to full range", "1996", "1996-01-01", "1996-12-31"},
		{"month expands within the month", "1996-06", "1996-06-01", "1996-06-31"},
		{"full date is left alone", "1996-06-15", "1996-06-15", "1996-06-15"},
		{"surrounding space is trimmed", "  2024  ", "2024-01-01", "2024-12-31"},
		{"unrecognised shape passes through", "not-a-date", "not-a-date", "not-a-date"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normaliseDate(tt.input); got != tt.wantFrom {
				t.Errorf("normaliseDate(%q) = %q, want %q", tt.input, got, tt.wantFrom)
			}
			if got := normaliseDateTo(tt.input); got != tt.wantTo {
				t.Errorf("normaliseDateTo(%q) = %q, want %q", tt.input, got, tt.wantTo)
			}
		})
	}
}

func TestRankMapOrdersByCountThenName(t *testing.T) {
	got := rankMap(map[string]int{
		"techno":  10,
		"house":   10,
		"ambient": 25,
		"dub":     3,
	})

	want := []rankEntry{
		{"ambient", 25},
		// Equal counts fall back to alphabetical order, so the list is stable
		// between runs despite Go's randomised map iteration.
		{"house", 10},
		{"techno", 10},
		{"dub", 3},
	}

	if len(got) != len(want) {
		t.Fatalf("rankMap() returned %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rankMap() = %v, want %v", got, want)
		}
	}
}

func TestRankMapEmpty(t *testing.T) {
	if got := rankMap(nil); len(got) != 0 {
		t.Errorf("rankMap(nil) = %v, want empty", got)
	}
}

func TestNewEntityStatsReadsFacets(t *testing.T) {
	facets := &beatport.Facets{Fields: map[string][]beatport.Facet{
		"genre":     {{ID: 1, Name: "Techno", Count: 40}},
		"sub_genre": {{ID: 9, Name: "Peak Time", Count: 12}},
		"artists":   {{ID: 5, Name: "Deadmau5", Count: 7}},
	}}

	stats := newEntityStats(99, facets)

	if stats.total != 99 {
		t.Errorf("total = %d, want 99", stats.total)
	}
	if stats.genres["Techno"] != 40 {
		t.Errorf("genres[Techno] = %d, want 40", stats.genres["Techno"])
	}
	if stats.subgenres["Peak Time"] != 12 {
		t.Errorf("subgenres[Peak Time] = %d, want 12", stats.subgenres["Peak Time"])
	}
	// The subgenre id is what the filter wizard puts in the query string.
	if stats.subgenreIds["Peak Time"] != 9 {
		t.Errorf("subgenreIds[Peak Time] = %d, want 9", stats.subgenreIds["Peak Time"])
	}
	if stats.artists["Deadmau5"] != 7 {
		t.Errorf("artists[Deadmau5] = %d, want 7", stats.artists["Deadmau5"])
	}
}

// Missing facet groups are normal for some entities and must not panic.
func TestNewEntityStatsHandlesMissingFacets(t *testing.T) {
	stats := newEntityStats(0, &beatport.Facets{})
	if len(stats.genres) != 0 || len(stats.subgenres) != 0 || len(stats.artists) != 0 {
		t.Errorf("expected empty stats, got %+v", stats)
	}
}
