package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"

	"github.com/monomadic/beetroot-dl/internal/beatport"
	"github.com/monomadic/beetroot-dl/internal/validator"

	"github.com/BurntSushi/toml"
)

type AppConfig struct {
	Username      string `toml:"username"`
	Password      string `toml:"password"`
	Quality       string `toml:"quality"`
	WriteErrorLog bool   `toml:"write_error_log"`
	ShowProgress  bool   `toml:"show_progress"`

	MaxGlobalWorkers   int `toml:"max_global_workers"`
	MaxDownloadWorkers int `toml:"max_download_workers"`

	DownloadsDirectory      string `toml:"downloads_directory"`
	SortByContext           bool   `toml:"sort_by_context"`
	SortByLabel             bool   `toml:"sort_by_label"`
	ForceReleaseDirectories bool   `toml:"force_release_directories"`
	TrackExists             string `toml:"track_exists"`
	TrackNumberPadding      int    `toml:"track_number_padding"`

	ReleaseDirectoryTemplate  string `toml:"release_directory_template"`
	PlaylistDirectoryTemplate string `toml:"playlist_directory_template"`
	ChartDirectoryTemplate    string `toml:"chart_directory_template"`
	LabelDirectoryTemplate    string `toml:"label_directory_template"`
	ArtistDirectoryTemplate   string `toml:"artist_directory_template"`
	TrackFileTemplate         string `toml:"track_file_template"`
	WhitespaceCharacter       string `toml:"whitespace_character"`
	ArtistsLimit              int    `toml:"artists_limit"`
	ArtistsShortForm          string `toml:"artists_short_form"`
	KeySystem                 string `toml:"key_system"`

	CoverSize string `toml:"cover_size"`
	KeepCover bool   `toml:"keep_cover"`
	FixTags   bool   `toml:"fix_tags"`

	TagMappings map[string]map[string]string `toml:"tag_mappings"`

	Proxy string `toml:"proxy"`
}

const (
	DefaultCoverSize = "1400x1400"
)

var (
	SupportedTrackExistsOptions = []string{
		"error",
		"skip",
		"overwrite",
		"update",
	}

	SupportedKeySystems = []string{
		"standard",
		"standard-short",
		"openkey",
		"camelot",
	}

	SupportedQualities = []string{
		"medium-hls",
		"medium",
		"high",
		"lossless",
	}
)

func FFMPEGInstalled() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// Defaults returns a config with every optional field populated. Parse starts
// from it before decoding, and the first-run wizard builds on it too — a config
// written from a bare AppConfig would serialise zero values ("", 0) that then
// override these defaults on the next load, failing validation.
func Defaults() AppConfig {
	return AppConfig{
		Quality:                   "lossless",
		CoverSize:                 DefaultCoverSize,
		TrackFileTemplate:         "{number}. {artists} - {name} ({mix_name})",
		ReleaseDirectoryTemplate:  "[{catalog_number}] {artists} - {name}",
		PlaylistDirectoryTemplate: "{name} [{created_date}]",
		ChartDirectoryTemplate:    "{name} [{published_date}]",
		LabelDirectoryTemplate:    "{name} [{updated_date}]",
		ArtistDirectoryTemplate:   "{name}",
		ArtistsLimit:              3,
		ArtistsShortForm:          "VA",
		KeySystem:                 "standard-short",
		TrackExists:               "update",
		TrackNumberPadding:        2,
		FixTags:                   true,
		ShowProgress:              true,
		MaxGlobalWorkers:          15,
		MaxDownloadWorkers:        15,
	}
}

func Parse(filePath string) (*AppConfig, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	config := Defaults()
	if _, err := toml.NewDecoder(file).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config file: %w", err)
	}

	if config.Username == "" || config.Password == "" {
		return nil, fmt.Errorf("username or password is not provided")
	}

	// Without this an unrecognised quality silently fell through to the
	// default download path, so a typo quietly changed what you got.
	if !validator.PermittedValue(config.Quality, SupportedQualities...) {
		return nil, fmt.Errorf("invalid quality %q (supported: %s)",
			config.Quality, strings.Join(SupportedQualities, ", "))
	}

	if config.Quality == "medium-hls" && !FFMPEGInstalled() {
		return nil, errors.New("ffmpeg not found")
	}

	if config.TagMappings != nil {
		if err = ValidateTagMappings(config.TagMappings); err != nil {
			return nil, err
		}

		if _, ok := config.TagMappings["flac"]; !ok {
			config.TagMappings["flac"] = DefaultTagMappings["flac"]
		}

		if _, ok := config.TagMappings["m4a"]; !ok {
			config.TagMappings["m4a"] = DefaultTagMappings["m4a"]
		}
	} else {
		config.TagMappings = DefaultTagMappings
	}

	if !validator.PermittedValue(config.KeySystem, SupportedKeySystems...) {
		return nil, fmt.Errorf("invalid key system %q (supported: %s)",
			config.KeySystem, strings.Join(SupportedKeySystems, ", "))
	}

	if config.DownloadsDirectory == "" {
		return nil, fmt.Errorf("no downloads directory provided")
	}

	if !validator.PermittedValue(config.TrackExists, SupportedTrackExistsOptions...) {
		return nil, fmt.Errorf("invalid track exists behavior %q (supported: %s)",
			config.TrackExists, strings.Join(SupportedTrackExistsOptions, ", "))
	}

	if config.TrackNumberPadding > 10 || config.TrackNumberPadding < 0 {
		return nil, fmt.Errorf("invalid track number padding")
	}

	// A zero or negative worker count builds a zero-capacity semaphore, and
	// every download then blocks forever on it — a silent hang rather than an
	// error, so reject it here.
	if config.MaxGlobalWorkers < 1 {
		return nil, fmt.Errorf("max_global_workers must be at least 1, got %d", config.MaxGlobalWorkers)
	}
	if config.MaxDownloadWorkers < 1 {
		return nil, fmt.Errorf("max_download_workers must be at least 1, got %d", config.MaxDownloadWorkers)
	}

	if config.ArtistsLimit < 1 {
		return nil, fmt.Errorf("artists_limit must be at least 1, got %d", config.ArtistsLimit)
	}

	if err := validateCoverSize(config.CoverSize); err != nil {
		return nil, err
	}

	if err := validateTemplates(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// maxCoverDimension is the largest size the Beatport image CDN will serve.
const maxCoverDimension = 1400

// validateCoverSize accepts the WIDTHxHEIGHT form the image URL builder
// expects. An unparseable value used to be substituted into the cover URL
// verbatim, which failed later as an opaque download error.
func validateCoverSize(size string) error {
	width, height, found := strings.Cut(size, "x")
	if !found {
		return fmt.Errorf("invalid cover_size %q (expected WIDTHxHEIGHT, e.g. %s)", size, DefaultCoverSize)
	}

	for _, part := range []struct {
		name  string
		value string
	}{{"width", width}, {"height", height}} {
		n, err := strconv.Atoi(part.value)
		if err != nil {
			return fmt.Errorf("invalid cover_size %q: %s is not a number", size, part.name)
		}
		if n < 1 || n > maxCoverDimension {
			return fmt.Errorf("invalid cover_size %q: %s must be between 1 and %d",
				size, part.name, maxCoverDimension)
		}
	}
	return nil
}

// validateTemplates rejects unknown {placeholders}. ParseTemplate leaves an
// unrecognised placeholder in the output verbatim, so without this a typo like
// {artist} ends up in every filename literally.
func validateTemplates(config *AppConfig) error {
	templates := []struct {
		option   string
		value    string
		keywords []string
	}{
		{"track_file_template", config.TrackFileTemplate, beatport.TrackTemplateKeywords()},
		{"release_directory_template", config.ReleaseDirectoryTemplate, beatport.ReleaseTemplateKeywords()},
		{"playlist_directory_template", config.PlaylistDirectoryTemplate, beatport.PlaylistTemplateKeywords()},
		{"chart_directory_template", config.ChartDirectoryTemplate, beatport.ChartTemplateKeywords()},
		{"label_directory_template", config.LabelDirectoryTemplate, beatport.LabelTemplateKeywords()},
		{"artist_directory_template", config.ArtistDirectoryTemplate, beatport.ArtistTemplateKeywords()},
	}

	for _, tmpl := range templates {
		for _, placeholder := range beatport.TemplatePlaceholders(tmpl.value) {
			if !validator.PermittedValue(placeholder, tmpl.keywords...) {
				return fmt.Errorf("unknown placeholder {%s} in %s (available: %s)",
					placeholder, tmpl.option, strings.Join(tmpl.keywords, ", "))
			}
		}
	}
	return nil
}

func (c *AppConfig) Save(filePath string) error {
	err := os.MkdirAll(path.Dir(filePath), 0700)
	if err != nil {
		return fmt.Errorf("could not create folder for configuration file: %w", err)
	}

	// 0600: this file holds the account password in plaintext, so it must not
	// be readable by other users on the machine (os.Create would give 0644).
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer file.Close()

	// O_CREATE only applies the mode to a *new* file, so tighten explicitly:
	// anyone upgrading has an existing config left at 0644 by earlier versions.
	if err := file.Chmod(0600); err != nil && !errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("tighten config file permissions: %w", err)
	}

	if err := toml.NewEncoder(file).Encode(c); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return nil
}
