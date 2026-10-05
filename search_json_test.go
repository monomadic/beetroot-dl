package main

import (
	"encoding/json"
	"testing"

	"github.com/monomadic/beetroot-dl/config"
	"github.com/monomadic/beetroot-dl/internal/beatport"
)

func TestSearchTrackProjection(t *testing.T) {
	var track beatport.Track
	data := `{"id":1696999,"name":"Strobe","mix_name":"Original Mix","slug":"strobe","artists":[{"name":"deadmau5"}],"bpm":128,"length_ms":635000,"publish_date":"2009-10-19","key":{"letter":"D","chord_type":{"name":"Minor"}},"release":{"label":{"name":"mau5trap"},"image":{"dynamic_uri":"https://img/{w}x{h}/cover.jpg"}}}`
	if err := json.Unmarshal([]byte(data), &track); err != nil {
		t.Fatal(err)
	}
	h := searchTrack(track, &config.AppConfig{ArtistsLimit: 3})
	if h.ID != 1696999 || h.Artists != "deadmau5" || h.LengthS != 635 || h.Year != 2009 || h.Key.Chord != "Minor" || h.CoverURL != "https://img/250x250/cover.jpg" || h.URL != "https://www.beatport.com/track/strobe/1696999" {
		t.Fatalf("unexpected search projection: %+v", h)
	}
}
