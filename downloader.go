package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/monomadic/beetroot-dl/config"
	"github.com/monomadic/beetroot-dl/internal/beatport"
	"go.senan.xyz/taglib"
	"os"
	"path/filepath"
	"resenje.org/singleflight"
	"strconv"
	"strings"
	"sync"
)

func (app *application) errorLogWrapper(url, step string, err error) {
	app.LogError(fmt.Sprintf("[%s] %s", url, step), err)
}

func (app *application) infoLogWrapper(url, message string) {
	app.LogInfo(fmt.Sprintf("[%s] %s", url, message))
}

func (app *application) createDirectory(baseDir string, subDir ...string) (string, error) {
	fullPath := filepath.Join(baseDir, filepath.Join(subDir...))
	err := CreateDirectory(fullPath)
	return fullPath, err
}

type DownloadsDirectoryEntity interface {
	DirectoryName(n beatport.NamingPreferences) string
}

func (app *application) setupDownloadsDirectory(baseDir string, entity DownloadsDirectoryEntity) (string, error) {
	if app.config.SortByContext {
		var subDir string
		switch castedEntity := entity.(type) {
		case *beatport.Release:
			subDir = castedEntity.DirectoryName(
				beatport.NamingPreferences{
					Template:           app.config.ReleaseDirectoryTemplate,
					Whitespace:         app.config.WhitespaceCharacter,
					ArtistsLimit:       app.config.ArtistsLimit,
					ArtistsShortForm:   app.config.ArtistsShortForm,
					TrackNumberPadding: app.config.TrackNumberPadding,
				},
			)
			if app.config.SortByLabel && entity != nil {
				baseDir = filepath.Join(baseDir, castedEntity.Label.Name)
			}
		case *beatport.Playlist:
			subDir = castedEntity.DirectoryName(
				beatport.NamingPreferences{
					Template:           app.config.PlaylistDirectoryTemplate,
					Whitespace:         app.config.WhitespaceCharacter,
					TrackNumberPadding: app.config.TrackNumberPadding,
				},
			)
		case *beatport.Chart:
			subDir = castedEntity.DirectoryName(
				beatport.NamingPreferences{
					Template:           app.config.ChartDirectoryTemplate,
					Whitespace:         app.config.WhitespaceCharacter,
					TrackNumberPadding: app.config.TrackNumberPadding,
				},
			)
		case *beatport.Label:
			subDir = castedEntity.DirectoryName(
				beatport.NamingPreferences{
					Template:   app.config.LabelDirectoryTemplate,
					Whitespace: app.config.WhitespaceCharacter,
				},
			)
		case *beatport.Artist:
			subDir = castedEntity.DirectoryName(
				beatport.NamingPreferences{
					Template:   app.config.ArtistDirectoryTemplate,
					Whitespace: app.config.WhitespaceCharacter,
				},
			)
		}
		baseDir = filepath.Join(baseDir, subDir)
	}
	return app.createDirectory(baseDir)
}

func (app *application) requireCover(respectFixTags, respectKeepCover bool) bool {
	fixTags := respectFixTags && app.config.FixTags &&
		(app.config.CoverSize != config.DefaultCoverSize || app.config.Quality != "lossless")
	keepCover := respectKeepCover && app.config.SortByContext && app.config.KeepCover
	return fixTags || keepCover
}

func (app *application) downloadCover(image beatport.Image, downloadsDir string) (string, error) {
	coverUrl := image.FormattedUrl(app.config.CoverSize)
	coverPath := filepath.Join(downloadsDir, uuid.New().String())
	err := app.downloadFile(coverUrl, coverPath, "")
	if err != nil {
		os.Remove(coverPath)
		return "", err
	}
	return coverPath, nil
}

func (app *application) handleCoverFile(path string) error {
	if path == "" {
		return nil
	}
	if app.config.KeepCover && app.config.SortByContext {
		newPath := filepath.Join(filepath.Dir(path), "cover.jpg")
		if err := os.Rename(path, newPath); err != nil {
			return err
		}
	} else {
		os.Remove(path)
	}
	return nil
}

var (
	ErrTrackFileExists = errors.New("file already exists")
)

// uniqueFilePath finds a free "<name> (n)<ext>" beside an existing file. It is
// reached when another worker in this run is already writing that exact name,
// so the two must not land on the same path.
func uniqueFilePath(directory, fileName, fileExtension string) string {
	for i := 1; ; i++ {
		candidate := filepath.Join(directory, fmt.Sprintf("%s (%d)%s", fileName, i, fileExtension))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func (app *application) saveTrack(track *beatport.Track, directory string, quality string) (string, error) {
	var fileExtension string
	var displayQuality string

	var stream *beatport.TrackStream
	var download *beatport.TrackDownload

	app.LogVerbose("requesting stream for %s (%s)", track.Name.String(), track.MixName.String())

	switch app.config.Quality {
	case "medium-hls":
		trackStream, err := app.bp.StreamTrack(track.ID)
		if err != nil {
			return "", err
		}
		fileExtension = ".m4a"
		displayQuality = "AAC 128kbps - HLS"
		stream = trackStream
	default:
		trackDownload, err := app.bp.DownloadTrack(track.ID, quality)
		if err != nil {
			return "", err
		}
		switch trackDownload.StreamQuality {
		case ".128k.aac.mp4":
			fileExtension = ".m4a"
			displayQuality = "AAC 128kbps"
		case ".256k.aac.mp4":
			fileExtension = ".m4a"
			displayQuality = "AAC 256kbps"
		case ".flac":
			fileExtension = ".flac"
			displayQuality = "FLAC"
		default:
			return "", fmt.Errorf("invalid stream quality: %s", trackDownload.StreamQuality)
		}
		download = trackDownload
	}

	fileName := track.Filename(
		beatport.NamingPreferences{
			Template:           app.config.TrackFileTemplate,
			Whitespace:         app.config.WhitespaceCharacter,
			ArtistsLimit:       app.config.ArtistsLimit,
			ArtistsShortForm:   app.config.ArtistsShortForm,
			TrackNumberPadding: app.config.TrackNumberPadding,
			KeySystem:          app.config.KeySystem,
		},
	)
	filePath := filepath.Join(directory, fileName+fileExtension)
	if _, err := os.Stat(filePath); err == nil {
		app.activeFilesMutex.RLock()
		_, exists := app.activeFiles[filePath]
		app.activeFilesMutex.RUnlock()

		if exists {
			filePath = uniqueFilePath(directory, fileName, fileExtension)
		} else {
			switch app.config.TrackExists {
			case "skip":
				app.LogVerbose("already present, skipping: %s", filePath)
				return "", nil
			case "update":
				app.infoLogWrapper(track.StoreUrl(), "updating tags")
				app.LogVerbose("already present, updating tags: %s", filePath)
				return filePath, nil
			case "error":
				return "", ErrTrackFileExists
			}
		}
	}

	app.activeFilesMutex.Lock()
	app.activeFiles[filePath] = struct{}{}
	app.activeFilesMutex.Unlock()

	var prefix string
	infoDisplay := fmt.Sprintf("%s (%s) [%s]", track.Name.String(), track.MixName.String(), displayQuality)
	if app.config.ShowProgress {
		prefix = infoDisplay
	} else {
		fmt.Fprintln(app.diagnosticWriter(), "Downloading "+infoDisplay)
	}

	app.LogVerbose("writing to %s", filePath)

	if download != nil {
		if err := app.downloadFile(download.Location, filePath, prefix); err != nil {
			os.Remove(filePath)
			return "", err
		}
	} else if stream != nil {
		segments, key, err := app.getStreamSegments(stream.Url)
		if err != nil {
			return "", fmt.Errorf("get stream segments: %v", err)
		}
		segmentsFile, err := app.downloadSegments(directory, segments, *key, prefix)
		if segmentsFile != "" {
			defer os.Remove(segmentsFile)
		}
		if err != nil {
			return "", fmt.Errorf("download segments: %v", err)
		}
		if err := remuxToM4A(segmentsFile, filePath); err != nil {
			os.Remove(filePath)
			return "", fmt.Errorf("remux to m4a: %v", err)
		}
	}

	if !app.config.ShowProgress {
		fmt.Fprintf(app.diagnosticWriter(), "Finished downloading %s\n", infoDisplay)
	}

	return filePath, nil
}

const (
	rawTagSuffix = "_raw"
)

func (app *application) tagTrack(location string, track *beatport.Track, coverPath string) error {
	fileExt := filepath.Ext(location)
	if !app.config.FixTags {
		return nil
	}

	subgenre := ""
	if track.Subgenre != nil {
		subgenre = track.Subgenre.Name
	}
	mappingValues := map[string]string{
		"track_id":       strconv.Itoa(int(track.ID)),
		"track_url":      track.StoreUrl(),
		"track_name":     fmt.Sprintf("%s (%s)", track.Name.String(), track.MixName.String()),
		"track_artists":  track.Artists.Display(0, ""),
		"track_remixers": track.Remixers.Display(0, ""),
		"track_artists_limited": track.Artists.Display(
			app.config.ArtistsLimit,
			app.config.ArtistsShortForm,
		),
		"track_remixers_limited": track.Remixers.Display(
			app.config.ArtistsLimit,
			app.config.ArtistsShortForm,
		),
		"track_number":              strconv.Itoa(track.Number),
		"track_number_with_padding": beatport.NumberWithPadding(track.Number, track.Release.TrackCount, app.config.TrackNumberPadding),
		"track_number_with_total":   fmt.Sprintf("%d/%d", track.Number, track.Release.TrackCount),
		"track_genre":               track.Genre.Name,
		"track_subgenre":            subgenre,
		"track_genre_with_subgenre": track.GenreWithSubgenre("|"),
		"track_subgenre_or_genre":   track.SubgenreOrGenre(),
		"track_key":                 track.Key.Display(app.config.KeySystem),
		"track_bpm":                 strconv.Itoa(track.BPM),
		"track_isrc":                track.ISRC,

		"release_id":   strconv.Itoa(int(track.Release.ID)),
		"release_url":  track.Release.StoreUrl(),
		"release_name": track.Release.Name.String(),
		"release_artists": track.Release.Artists.Display(
			0,
			"",
		),
		"release_remixers": track.Release.Remixers.Display(
			0,
			"",
		),
		"release_artists_limited": track.Release.Artists.Display(
			app.config.ArtistsLimit,
			app.config.ArtistsShortForm,
		),
		"release_remixers_limited": track.Release.Remixers.Display(
			app.config.ArtistsLimit,
			app.config.ArtistsShortForm,
		),
		"release_date":        track.Release.Date,
		"release_year":        track.Release.Year(),
		"release_track_count": strconv.Itoa(track.Release.TrackCount),
		"release_track_count_with_padding": beatport.NumberWithPadding(
			track.Release.TrackCount, track.Release.TrackCount, app.config.TrackNumberPadding,
		),
		"release_catalog_number": track.Release.CatalogNumber.String(),
		"release_upc":            track.Release.UPC,
		"release_label":          track.Release.Label.Name,
		"release_label_url":      track.Release.Label.StoreUrl(),
	}

	var format string
	switch fileExt {
	case ".flac":
		format = "flac"
	case ".m4a":
		format = "m4a"
	default:
		return fmt.Errorf("no tag support for %s files", fileExt)
	}

	tags := make(map[string][]string)
	for field, property := range app.config.TagMappings[format] {
		// Older configs could route an m4a field to a raw MP4 atom with a
		// `_raw` suffix. TagLib's property interface stores unknown keys as
		// freeform atoms — the same intent — so the suffix is just stripped.
		property = strings.TrimSuffix(property, rawTagSuffix)
		if value := mappingValues[field]; value != "" {
			tags[property] = []string{value}
		}
	}

	// Clear drops every existing tag not present in the new map — the same
	// wipe-then-write the CGO binding did with StripMp4/SetProperty(nil).
	if err := taglib.WriteTags(location, tags, taglib.Clear); err != nil {
		return fmt.Errorf("write tags: %w", err)
	}

	if coverPath != "" && (app.config.CoverSize != config.DefaultCoverSize || fileExt == ".m4a") {
		data, err := os.ReadFile(coverPath)
		if err != nil {
			return err
		}
		if err := taglib.WriteImageOptions(location, data, 0, "Front Cover", "Cover", "image/jpeg"); err != nil {
			return fmt.Errorf("write cover: %w", err)
		}
	}

	return nil
}

func (app *application) handleTrack(track *beatport.Track, downloadsDir string, coverPath string) error {
	location, err := app.saveTrack(track, downloadsDir, app.config.Quality)
	if err != nil {
		return fmt.Errorf("save track: %v", err)
	}
	if err = app.tagTrack(location, track, coverPath); err != nil && location != "" {
		return fmt.Errorf("tag track: %v", err)
	}
	// An empty location means the track was skipped as already-present; there
	// is no new file to report.
	if location != "" {
		app.LogVerbose("tagged %s", location)
		app.recordDownload(location)
	}
	return nil
}

func (app *application) cleanup(downloadsDir string) {
	if downloadsDir != app.config.DownloadsDirectory {
		os.Remove(downloadsDir)
	}
}

func ForPaginated[T any](
	entityId int64,
	params string,
	fetchPage func(id int64, page int, params string) (results *beatport.Paginated[T], err error),
	processItem func(item T, i int) error,
) error {
	page := 1
	for {
		paginated, err := fetchPage(entityId, page, params)
		if err != nil {
			return fmt.Errorf("fetch page: %w", err)
		}

		for i, item := range paginated.Results {
			if err := processItem(item, i); err != nil {
				return fmt.Errorf("process item: %w", err)
			}
		}

		if paginated.Next == nil {
			break
		}
		page++
	}
	return nil
}

func (app *application) handleUrl(url string) {
	link, err := app.bp.ParseUrl(url)
	if err != nil {
		app.errorLogWrapper(url, "parse url", err)
		return
	}

	app.LogVerbose("resolved %s -> %s id=%d", url, link.Type, link.ID)

	switch link.Type {
	case beatport.TrackLink:
		app.handleTrackLink(link)
	case beatport.ReleaseLink:
		app.handleReleaseLink(link)
	case beatport.PlaylistLink:
		app.handlePlaylistLink(link)
	case beatport.ChartLink:
		app.handleChartLink(link)
	case beatport.LabelLink:
		app.handleLabelLink(link)
	case beatport.ArtistLink:
		app.handleArtistLink(link)
	default:
		app.LogError("handle URL", ErrUnsupportedLinkType)
	}
}

func (app *application) handleTrackLink(link *beatport.Link) {
	app.LogVerbose("fetching track metadata (id=%d)", link.ID)
	track, err := app.bp.GetTrack(link.ID)
	if err != nil {
		app.errorLogWrapper(link.Original, "fetch track", err)
		return
	}

	app.LogVerbose("fetching release metadata (id=%d)", track.Release.ID)
	release, err := app.bp.GetRelease(track.Release.ID)
	if err != nil {
		app.errorLogWrapper(link.Original, "fetch track release", err)
		return
	}
	track.Release = *release

	downloadsDir, err := app.setupDownloadsDirectory(app.config.DownloadsDirectory, release)
	if err != nil {
		app.errorLogWrapper(link.Original, "setup downloads directory", err)
		return
	}
	app.LogVerbose("downloads directory: %s", downloadsDir)

	wg := sync.WaitGroup{}
	app.downloadWorker(&wg, func() {
		var cover string
		if app.requireCover(true, true) {
			cover, err = app.downloadCover(track.Release.Image, downloadsDir)
			if err != nil {
				app.errorLogWrapper(link.Original, "download track release cover", err)
			}
		}

		if err := app.handleTrack(track, downloadsDir, cover); err != nil {
			app.errorLogWrapper(link.Original, "handle track", err)
			os.Remove(cover)
			return
		}

		if err := app.handleCoverFile(cover); err != nil {
			app.errorLogWrapper(link.Original, "handle cover file", err)
			return
		}
	})
	wg.Wait()

	app.cleanup(downloadsDir)
}

func (app *application) handleReleaseLink(link *beatport.Link) {
	release, err := app.bp.GetRelease(link.ID)
	if err != nil {
		app.errorLogWrapper(link.Original, "fetch release", err)
		return
	}

	downloadsDir, err := app.setupDownloadsDirectory(app.config.DownloadsDirectory, release)
	if err != nil {
		app.errorLogWrapper(link.Original, "setup downloads directory", err)
		return
	}

	var cover string
	if app.requireCover(true, true) {
		app.semAcquire(app.downloadSem)
		cover, err = app.downloadCover(release.Image, downloadsDir)
		if err != nil {
			app.errorLogWrapper(link.Original, "download release cover", err)
		}
		app.semRelease(app.downloadSem)
	}

	wg := sync.WaitGroup{}
	for _, trackUrl := range release.TrackUrls {
		app.downloadWorker(&wg, func() {
			trackLink, err := app.bp.ParseUrl(trackUrl)
			if err != nil {
				app.errorLogWrapper(link.Original, "parse track url", err)
				return
			}

			track, err := app.bp.GetTrack(trackLink.ID)
			if err != nil {
				app.errorLogWrapper(trackUrl, "fetch release track", err)
				return
			}
			trackStoreUrl := track.StoreUrl()
			track.Release = *release

			if err := app.handleTrack(track, downloadsDir, cover); err != nil {
				app.errorLogWrapper(trackStoreUrl, "handle track", err)
				return
			}
		})
	}
	wg.Wait()

	if err := app.handleCoverFile(cover); err != nil {
		app.errorLogWrapper(link.Original, "handle cover file", err)
		return
	}

	app.cleanup(downloadsDir)
}

// trackList describes a link whose payload is a paginated list of tracks —
// playlists, charts, labels and artists. They differ only in how the list is
// enumerated and how the resulting files are laid out, so they share one
// pipeline; keeping four near-identical copies is what let the chart and label
// handlers drift into reporting "handle playlist items" on failure.
type trackList struct {
	// entity names the top-level directory for the whole list.
	entity DownloadsDirectoryEntity

	// contextCover, when set, is artwork for the list directory itself rather
	// than for any single release (charts have one; labels do not).
	contextCover *beatport.Image

	// alwaysReleaseDirs gives every track its own release subdirectory even
	// when force_release_directories is off. Labels and artists span many
	// releases, so flattening them into one directory would collide.
	alwaysReleaseDirs bool

	// step labels pagination failures in the error log.
	step string

	// forEachTrack drives pagination, calling yield once per track id.
	forEachTrack func(yield func(trackID int64)) error
}

func (app *application) handleTrackList(link *beatport.Link, list trackList) {
	downloadsDir, err := app.setupDownloadsDirectory(app.config.DownloadsDirectory, list.entity)
	if err != nil {
		app.errorLogWrapper(link.Original, "setup downloads directory", err)
		return
	}
	app.LogVerbose("downloads directory: %s", downloadsDir)

	wg := sync.WaitGroup{}

	if list.contextCover != nil && app.requireCover(false, true) {
		cover := *list.contextCover
		app.downloadWorker(&wg, func() {
			path, err := app.downloadCover(cover, downloadsDir)
			if err != nil {
				app.errorLogWrapper(link.Original, "download cover", err)
				return
			}
			if err := app.handleCoverFile(path); err != nil {
				app.errorLogWrapper(link.Original, "handle cover file", err)
			}
		})
	}

	// Tracks in a list frequently share a release; without this, every track
	// of the same release would refetch it.
	var releases singleflight.Group[int64, *beatport.Release]
	releaseDirs := app.config.ForceReleaseDirectories || list.alwaysReleaseDirs

	err = list.forEachTrack(func(trackID int64) {
		app.downloadWorker(&wg, func() {
			app.handleListTrack(trackID, downloadsDir, releaseDirs, &releases)
		})
	})
	if err != nil {
		app.errorLogWrapper(link.Original, list.step, err)
		// Deliberately no early return: tracks queued before the failing page
		// are already downloading, and abandoning them here would let the run
		// finish — and tear down the progress bars — underneath them.
	}

	wg.Wait()
}

// handleListTrack resolves one track of a list and writes it to disk.
func (app *application) handleListTrack(
	trackID int64,
	downloadsDir string,
	releaseDirs bool,
	releases *singleflight.Group[int64, *beatport.Release],
) {
	track, err := app.bp.GetTrack(trackID)
	if err != nil {
		app.errorLogWrapper(fmt.Sprintf("track %d", trackID), "fetch track", err)
		return
	}
	trackStoreUrl := track.StoreUrl()

	release, _, err := releases.Do(app.ctx, track.Release.ID, func(ctx context.Context) (*beatport.Release, error) {
		return app.bp.GetRelease(track.Release.ID)
	})
	if err != nil {
		app.errorLogWrapper(trackStoreUrl, "fetch track release", err)
		return
	}
	track.Release = *release

	trackDir := downloadsDir
	if app.config.SortByContext && releaseDirs {
		trackDir, err = app.setupDownloadsDirectory(downloadsDir, release)
		if err != nil {
			app.errorLogWrapper(trackStoreUrl, "setup track release directory", err)
			return
		}
	}

	var cover string
	if app.requireCover(true, releaseDirs) {
		cover, err = app.downloadCover(release.Image, trackDir)
		if err != nil {
			app.errorLogWrapper(trackStoreUrl, "download track release cover", err)
		}
	}

	if err := app.handleTrack(track, trackDir, cover); err != nil {
		app.errorLogWrapper(trackStoreUrl, "handle track", err)
		os.Remove(cover)
		app.cleanup(trackDir)
		return
	}

	if releaseDirs {
		// The cover belongs to this directory, so keep it (subject to
		// keep_cover) rather than discarding it after tagging.
		if err := app.handleCoverFile(cover); err != nil {
			app.errorLogWrapper(trackStoreUrl, "handle track release cover file", err)
			return
		}
	} else {
		// Tracks from many releases share one directory here, so a single
		// cover.jpg would be wrong; it was only fetched for tagging.
		os.Remove(cover)
	}

	app.cleanup(trackDir)
}

func (app *application) handlePlaylistLink(link *beatport.Link) {
	playlist, err := app.bp.GetPlaylist(link.ID)
	if err != nil {
		app.errorLogWrapper(link.Original, "fetch playlist", err)
		return
	}

	app.handleTrackList(link, trackList{
		entity: playlist,
		step:   "handle playlist items",
		forEachTrack: func(yield func(trackID int64)) error {
			return ForPaginated(link.ID, "", app.bp.GetPlaylistItems,
				func(item beatport.PlaylistItem, _ int) error {
					yield(item.Track.ID)
					return nil
				})
		},
	})
}

func (app *application) handleChartLink(link *beatport.Link) {
	chart, err := app.bp.GetChart(link.ID)
	if err != nil {
		app.errorLogWrapper(link.Original, "fetch chart", err)
		return
	}

	app.handleTrackList(link, trackList{
		entity:       chart,
		contextCover: &chart.Image,
		step:         "handle chart tracks",
		forEachTrack: func(yield func(trackID int64)) error {
			return ForPaginated(link.ID, "", app.bp.GetChartTracks,
				func(track beatport.Track, _ int) error {
					yield(track.ID)
					return nil
				})
		},
	})
}

func (app *application) handleLabelLink(link *beatport.Link) {
	label, err := app.bp.GetLabel(link.ID)
	if err != nil {
		app.errorLogWrapper(link.Original, "fetch label", err)
		return
	}

	app.handleTrackList(link, trackList{
		entity:            label,
		alwaysReleaseDirs: true,
		step:              "handle label releases",
		forEachTrack: func(yield func(trackID int64)) error {
			return ForPaginated(link.ID, link.Params, app.bp.GetLabelTracks,
				func(track beatport.Track, _ int) error {
					yield(track.ID)
					return nil
				})
		},
	})
}

func (app *application) handleArtistLink(link *beatport.Link) {
	artist, err := app.bp.GetArtist(link.ID)
	if err != nil {
		app.errorLogWrapper(link.Original, "fetch artist", err)
		return
	}

	app.handleTrackList(link, trackList{
		entity:            artist,
		alwaysReleaseDirs: true,
		step:              "handle artist tracks",
		forEachTrack: func(yield func(trackID int64)) error {
			return ForPaginated(link.ID, link.Params, app.bp.GetArtistTracks,
				func(track beatport.Track, _ int) error {
					yield(track.ID)
					return nil
				})
		},
	})
}
