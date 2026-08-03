package main

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/monomadic/beetroot-dl/config"
	"github.com/monomadic/beetroot-dl/internal/beatport"
	"net/url"
	"os"
	"strconv"
	"strings"
)

var (
	ErrUnsupportedLinkType = errors.New("unsupported link type")
)

func Setup(configPathOverride string, interactive bool) (cfg *config.AppConfig, cachePath string, err error) {
	var configFilePath string
	var exists bool

	if configPathOverride != "" {
		configFilePath = configPathOverride
		if _, statErr := os.Stat(configFilePath); statErr != nil {
			return nil, configFilePath, fmt.Errorf("config file not found: %s", configFilePath)
		}
		exists = true
	} else {
		configFilePath, exists, err = FindConfigFile()
		if err != nil {
			return nil, "", err
		}
	}

	if !exists {
		// Creating a config means prompting for credentials, which can only
		// happen with a human present. Failing loudly beats blocking forever
		// on stdin inside a cron job or a pipeline.
		if !interactive && !isTerminal(os.Stdin) {
			return nil, configFilePath, fmt.Errorf(
				"no config file at %s\ncreate one by running: beetroot-dl -i", configFilePath)
		}
		fmt.Fprintln(os.Stderr, "Config file not found, creating a new one:", configFilePath)

		fmt.Fprint(os.Stderr, "Username: ")
		username := GetLine()
		fmt.Fprint(os.Stderr, "Password: ")
		password := GetLine()
		fmt.Fprint(os.Stderr, "Downloads directory: ")
		downloadsDir := GetLine()

		// Start from the defaults: a bare AppConfig would write out zero values
		// for every field we don't prompt for, and those explicit empties
		// override the defaults Parse applies, failing validation immediately.
		defaults := config.Defaults()
		cfg := &defaults
		cfg.Username = username
		cfg.Password = password
		cfg.DownloadsDirectory = downloadsDir

		fmt.Fprintln(os.Stderr, "1. Lossless (44.1 khz FLAC)\n2. High (256 kbps AAC)\n3. Medium (128 kbps AAC)\n4. Medium HLS (128 kbps AAC)")
		for {
			fmt.Fprint(os.Stderr, "Quality: ")
			qualityNumber := GetLine()
			switch qualityNumber {
			case "1":
				cfg.Quality = "lossless"
			case "2":
				cfg.Quality = "high"
			case "3":
				cfg.Quality = "medium"
			case "4":
				cfg.Quality = "medium-hls"
			default:
				fmt.Fprintln(os.Stderr, "Invalid quality")
				continue
			}
			break
		}

		if err := cfg.Save(configFilePath); err != nil {
			return nil, configFilePath, fmt.Errorf("save config: %w", err)
		}
	}

	parsedConfig, err := config.Parse(configFilePath)
	if err != nil {
		return nil, configFilePath, fmt.Errorf("load config: %w", err)
	}

	cacheFilePath, _, err := FindCacheFile()
	if err != nil {
		return nil, configFilePath, fmt.Errorf("get executable path: %w", err)
	}

	return parsedConfig, cacheFilePath, nil
}

func (app *application) mainPrompt() {
	fmt.Fprint(os.Stderr, "Enter url or search query: ")
	input := GetLine()
	if strings.HasPrefix(input, "https://www.beatport.com") {
		if strings.Contains(input, "/label/") || strings.Contains(input, "/artist/") {
			app.filtersPrompt(input)
		} else {
			app.addUrls(input)
		}
	} else {
		app.search(input)
	}
}

func (app *application) filtersPrompt(rawURL string) {
	link, err := app.bp.ParseUrl(rawURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not parse URL:", err)
		return
	}

	params := "include_facets=true&per_page=1"

	var stats *entityStats
	var listItemName string
	switch link.Type {
	case beatport.LabelLink:
		listItemName = "releases"
		labelReleases, err := app.bp.GetLabelReleases(link.ID, 1, params)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Could not fetch label releases:", err)
			return
		}

		stats = newEntityStats(labelReleases.Count, &labelReleases.Facets)
	case beatport.ArtistLink:
		listItemName = "tracks"
		artistTracks, err := app.bp.GetArtistTracks(link.ID, 1, params)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Could not fetch artist tracks:", err)
			return
		}

		stats = newEntityStats(artistTracks.Count, &artistTracks.Facets)
	default:
		return
	}

	fmt.Fprintf(os.Stderr, "\n%d %s total.\n", stats.total, listItemName)

	genres := rankMap(stats.genres)
	subgenres := rankMap(stats.subgenres)
	artists := rankMap(stats.artists)

	const (
		stepGenres = iota
		stepSubgenres
		stepArtists
		stepDateFrom
		stepDateTo
		stepConfirm
	)

	var selectedGenres, selectedSubgenres, selectedArtists []string
	var dateFrom, dateTo string

	step := stepGenres
	for {
		switch step {

		case stepGenres:
			sel, back := selectFromList("\nGenres", genres)
			if back {
				fmt.Fprintln(os.Stderr, "Cancelled.")
				return
			}
			selectedGenres = sel
			step = stepSubgenres

		case stepSubgenres:
			if len(subgenres) == 0 {
				step = stepArtists
				continue
			}
			sel, back := selectFromList("\nSubgenres", subgenres)
			if back {
				step = stepGenres
				continue
			}
			selectedSubgenres = sel
			step = stepArtists

		case stepArtists:
			if len(artists) == 0 {
				step = stepDateFrom
				continue
			}
			sel, back := selectFromList("\nArtists (by track count)", artists)
			if back {
				if len(subgenres) > 0 {
					step = stepSubgenres
				} else {
					step = stepGenres
				}
				continue
			}
			selectedArtists = sel
			step = stepDateFrom

		case stepDateFrom:
			fmt.Fprint(os.Stderr, "\nDownload from date (e.g. 1996 or 1996-06-01, Enter for all, b to go back): ")
			input := strings.TrimSpace(GetLine())
			if input == "b" {
				if len(artists) > 0 {
					step = stepArtists
				} else if len(subgenres) > 0 {
					step = stepSubgenres
				} else {
					step = stepGenres
				}
				continue
			}
			dateFrom = normaliseDate(input)
			step = stepDateTo

		case stepDateTo:
			fmt.Fprint(os.Stderr, "Download up to date   (e.g. 2024 or 2024-12-31, Enter for all, b to go back): ")
			input := strings.TrimSpace(GetLine())
			if input == "b" {
				step = stepDateFrom
				continue
			}
			dateTo = normaliseDateTo(input)
			step = stepConfirm

		case stepConfirm:
			fmt.Fprintln(os.Stderr, "\n--- Download filter summary ---")
			if len(selectedGenres) > 0 {
				fmt.Fprintln(os.Stderr, "  Genres:    ", strings.Join(selectedGenres, ", "))
			} else {
				fmt.Fprintln(os.Stderr, "  Genres:     all")
			}
			if len(selectedSubgenres) > 0 {
				fmt.Fprintln(os.Stderr, "  Subgenres: ", strings.Join(selectedSubgenres, ", "))
			} else {
				fmt.Fprintln(os.Stderr, "  Subgenres:  all")
			}
			if len(selectedArtists) > 0 {
				fmt.Fprintln(os.Stderr, "  Artists:   ", strings.Join(selectedArtists, ", "))
			} else {
				fmt.Fprintln(os.Stderr, "  Artists:    all")
			}
			dateRange := "all time"
			if dateFrom != "" && dateTo != "" {
				dateRange = dateFrom + " → " + dateTo
			} else if dateFrom != "" {
				dateRange = dateFrom + " → present"
			} else if dateTo != "" {
				dateRange = "up to " + dateTo
			}
			fmt.Fprintln(os.Stderr, "  Dates:     ", dateRange)

			fmt.Fprint(os.Stderr, "\nStart download? (y/n/b to go back): ")
			ans := strings.ToLower(strings.TrimSpace(GetLine()))
			if ans == "b" {
				step = stepDateTo
				continue
			}
			if ans != "y" {
				fmt.Fprintln(os.Stderr, "Cancelled.")
				return
			}

			var params []string

			for i, genre := range selectedGenres {
				selectedGenres[i] = url.QueryEscape(genre)
			}
			genresStr := strings.Join(selectedGenres, ",")

			var subgenreIds []string
			for _, subgenre := range selectedSubgenres {
				subgenreIds = append(subgenreIds, strconv.FormatInt(stats.subgenreIds[subgenre], 10))
			}
			subgenreIdsStr := strings.Join(subgenreIds, ",")

			for i, artist := range selectedArtists {
				selectedArtists[i] = url.QueryEscape(artist)
			}
			artistsStr := strings.Join(selectedArtists, ",")

			if len(selectedGenres) > 0 {
				params = append(params, fmt.Sprintf("genre_name=%s", genresStr))
			}

			if len(selectedSubgenres) > 0 {
				params = append(params, fmt.Sprintf("sub_genre_id=%s", subgenreIdsStr))
			}

			if len(selectedArtists) > 0 {
				params = append(params, fmt.Sprintf("artist_name=%s", artistsStr))
			}

			if dateFrom != "" || dateTo != "" {
				params = append(params, fmt.Sprintf("new_release_date=%s:%s", dateFrom, dateTo))
			}

			paramsTotal := strings.Join(params, "&")

			if link.Params != "" {
				paramsTotal = fmt.Sprintf("&%s", paramsTotal)
			} else {
				paramsTotal = fmt.Sprintf("?%s", paramsTotal)
			}

			rawURL += paramsTotal

			app.addUrls(rawURL)
			return
		}
	}
}

// selectFromList prints a numbered list and returns the names the user chose plus a back flag.
// Returns nil on Enter & asterisk; back=true if user types b.
func selectFromList(heading string, entries []rankEntry) ([]string, bool) {
	if len(entries) == 0 {
		return nil, false
	}
	fmt.Fprintf(os.Stderr, "%s found:\n", heading)
	for i, e := range entries {
		fmt.Fprintf(os.Stderr, "  %2d. %-42s %d tracks\n", i+1, e.name, e.count)
	}
	fmt.Fprint(os.Stderr, "Select (e.g. 1,3  |  * for all  |  Enter to skip  |  b to go back): ")
	input := strings.TrimSpace(GetLine())

	if input == "b" {
		return nil, true
	}
	if input == "" || input == "*" {
		return nil, false
	}

	var selected []string
	for _, part := range strings.Split(input, ",") {
		part = strings.TrimSpace(part)
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > len(entries) {
			fmt.Fprintf(os.Stderr, "  (ignored invalid selection: %q)\n", part)
			continue
		}
		selected = append(selected, entries[n-1].name)
	}
	return selected, false
}

// normaliseDateFrom accepts "1996", "1996-06", or "1996-06-01" and returns "YYYY-MM-DD" (start of period).
func normaliseDate(input string) string {
	return normaliseDateBound(input, false)
}

// normaliseDateTo resolves to the end of the given year or month.
func normaliseDateTo(input string) string {
	return normaliseDateBound(input, true)
}

func normaliseDateBound(input string, endOfPeriod bool) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	switch len(input) {
	case 4: // "1996"
		if endOfPeriod {
			return input + "-12-31"
		}
		return input + "-01-01"
	case 7: // "1996-06"
		if endOfPeriod {
			return input + "-31" // good enough for string comparison purposes
		}
		return input + "-01"
	default:
		return input
	}
}

func (app *application) search(input string) {
	results, err := app.bp.Search(input)
	if err != nil {
		app.FatalError("beatport", err)
	}
	trackResultsLen := len(results.Tracks)
	releasesResultsLen := len(results.Releases)
	labelsResultsLen := len(results.Labels)
	totalResultsLen := trackResultsLen + releasesResultsLen + labelsResultsLen

	if totalResultsLen == 0 {
		fmt.Fprintln(os.Stderr, "No results found")
		return
	}

	fmt.Fprintln(os.Stderr, "Search results:")
	fmt.Fprintln(os.Stderr, "[ Tracks ]")
	for i, track := range results.Tracks {
		fmt.Fprintf(os.Stderr,
			"%2d. %s - %s (%s) [%s]\n", i+1,
			track.Artists.Display(app.config.ArtistsLimit, app.config.ArtistsShortForm),
			track.Name.String(), track.MixName.String(), track.Length,
		)
	}
	lastTrackNum := trackResultsLen

	fmt.Fprintln(os.Stderr, "\n[ Releases ]")
	for i, release := range results.Releases {
		fmt.Fprintf(os.Stderr,
			"%2d. %s - %s [%s]\n", i+lastTrackNum+1,
			release.Artists.Display(app.config.ArtistsLimit, app.config.ArtistsShortForm),
			release.Name.String(), release.Label.Name,
		)
	}
	lastReleaseNum := lastTrackNum + releasesResultsLen

	fmt.Fprintln(os.Stderr, "\n[ Labels ]")
	for i, label := range results.Labels {
		fmt.Fprintf(os.Stderr, "%2d. %s\n", i+lastReleaseNum+1, label.Name)
	}
	lastLabelNum := lastReleaseNum + labelsResultsLen

	fmt.Fprint(os.Stderr, "Enter the result number(s): ")
	input = GetLine()
	requestedResults := strings.Split(input, " ")

	for _, result := range requestedResults {
		nRes, err := strconv.Atoi(result)
		if err != nil || nRes <= 0 || nRes > totalResultsLen {
			fmt.Fprintf(os.Stderr, "invalid result number: %s\n", result)
			continue
		}

		if nRes <= lastTrackNum {
			app.addUrls(results.Tracks[nRes-1].URL)
			continue
		}

		if nRes <= lastReleaseNum {
			app.addUrls(results.Releases[nRes-1-lastTrackNum].URL)
			continue
		}

		if nRes <= lastLabelNum {
			app.addUrls(results.Labels[nRes-1-lastReleaseNum].StoreUrl())
			continue
		}
	}
}

func (app *application) parseTextFile(path string) {
	file, err := os.Open(path)
	if err != nil {
		app.FatalError("read input text file", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	for scanner.Scan() {
		app.addUrls(scanner.Text())
	}
}
