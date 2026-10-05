package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/monomadic/beetroot-dl/config"
	"github.com/monomadic/beetroot-dl/internal/beatport"
)

// searchHit is the stable, flat JSON Lines contract consumed by OnlineSource.
type searchHit struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	MixName  string  `json:"mix_name"`
	Artists  string  `json:"artists"`
	Remixers string  `json:"remixers"`
	Genre    string  `json:"genre"`
	Label    string  `json:"label"`
	BPM      int     `json:"bpm"`
	LengthS  float64 `json:"length_s"`
	Year     int     `json:"year"`
	Key      struct {
		Letter  string `json:"letter"`
		IsSharp bool   `json:"is_sharp"`
		IsFlat  bool   `json:"is_flat"`
		Chord   string `json:"chord"`
	} `json:"key"`
	CoverURL string `json:"cover_url"`
	URL      string `json:"url"`
}

func searchTrack(t beatport.Track, cfg *config.AppConfig) searchHit {
	h := searchHit{
		ID: t.ID, Name: t.Name.String(), MixName: t.MixName.String(),
		Artists:  t.Artists.Display(cfg.ArtistsLimit, cfg.ArtistsShortForm),
		Remixers: t.Remixers.Display(cfg.ArtistsLimit, cfg.ArtistsShortForm),
		Genre:    t.Genre.Name, Label: t.Release.Label.Name, BPM: t.BPM,
		LengthS: float64(t.LengthMs) / 1000, CoverURL: t.Release.Image.FormattedUrl("250x250"),
		URL: t.StoreUrl(),
	}
	if len(t.PublishDate) >= 4 {
		h.Year, _ = strconv.Atoi(t.PublishDate[:4])
	}
	h.Key.Letter, h.Key.IsSharp, h.Key.IsFlat, h.Key.Chord = t.Key.Letter, t.Key.IsSharp, t.Key.IsFlat, t.Key.ChordType.Name
	return h
}

func runSearchCommand(args []string, configPath string) {
	if len(args) < 2 || args[0] != "--json" || strings.TrimSpace(strings.Join(args[1:], " ")) == "" {
		fmt.Fprintln(os.Stderr, "usage: beetroot-dl search --json QUERY")
		os.Exit(2)
	}
	cfg, cachePath, err := Setup(configPath, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	auth := beatport.NewAuth(cfg.Username, cfg.Password, cachePath)
	bp, err := beatport.New(cfg.Proxy, auth)
	if err == nil {
		if err = auth.LoadCache(); err != nil {
			err = auth.Init(bp)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	results, err := bp.Search(strings.Join(args[1:], " "))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	for _, t := range results.Tracks {
		if err := enc.Encode(searchTrack(t, cfg)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
