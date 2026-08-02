package config

import (
	"errors"
	"fmt"
	"github.com/monomadic/beetroot-dl/internal/validator"
	"os"
	"os/exec"
	"path"

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
)

func FFMPEGInstalled() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

func Parse(filePath string) (*AppConfig, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	config := AppConfig{
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
	if _, err := toml.NewDecoder(file).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config file: %w", err)
	}

	if config.Username == "" || config.Password == "" {
		return nil, fmt.Errorf("username or password is not provided")
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
		return nil, fmt.Errorf("invalid key system")
	}

	if config.DownloadsDirectory == "" {
		return nil, fmt.Errorf("no downloads directory provided")
	}

	if !validator.PermittedValue(config.TrackExists, SupportedTrackExistsOptions...) {
		return nil, fmt.Errorf("invalid track exists behavior")
	}

	if config.TrackNumberPadding > 10 || config.TrackNumberPadding < 0 {
		return nil, fmt.Errorf("invalid track number padding")
	}

	return &config, nil
}

func (c *AppConfig) Save(filePath string) error {
	err := os.MkdirAll(path.Dir(filePath), 0700)
	if err != nil {
		return fmt.Errorf("could not create folder for configuration file: %w", err)
	}

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer file.Close()

	if err := toml.NewEncoder(file).Encode(c); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return nil
}
