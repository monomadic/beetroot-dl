package beatport

import "sort"

// The template keyword lists below are derived from the same templateValues
// maps the naming methods expand, so a placeholder that validates here is
// guaranteed to be one the formatter actually understands. Adding a value to a
// templateValues map is all it takes to make it configurable.

// TemplatePlaceholders returns the {placeholder} names used in a template, in
// order of appearance and without duplicates. Config validation uses it to
// reject a typo before it silently ends up in a filename verbatim.
func TemplatePlaceholders(template string) []string {
	matches := templatePlaceholderRegexp.FindAllStringSubmatch(template, -1)
	seen := make(map[string]struct{}, len(matches))
	placeholders := make([]string, 0, len(matches))
	for _, m := range matches {
		if _, ok := seen[m[1]]; ok {
			continue
		}
		seen[m[1]] = struct{}{}
		placeholders = append(placeholders, m[1])
	}
	return placeholders
}

func keywordsOf(values map[string]string) []string {
	keywords := make([]string, 0, len(values))
	for k := range values {
		keywords = append(keywords, k)
	}
	sort.Strings(keywords)
	return keywords
}

// TrackTemplateKeywords lists the placeholders valid in track_file_template.
func TrackTemplateKeywords() []string {
	return keywordsOf((&Track{}).templateValues(NamingPreferences{}))
}

// ReleaseTemplateKeywords lists the placeholders valid in
// release_directory_template.
func ReleaseTemplateKeywords() []string {
	return keywordsOf((&Release{}).templateValues(NamingPreferences{}))
}

// PlaylistTemplateKeywords lists the placeholders valid in
// playlist_directory_template.
func PlaylistTemplateKeywords() []string {
	return keywordsOf((&Playlist{}).templateValues(NamingPreferences{}))
}

// ChartTemplateKeywords lists the placeholders valid in
// chart_directory_template.
func ChartTemplateKeywords() []string {
	return keywordsOf((&Chart{}).templateValues(NamingPreferences{}))
}

// LabelTemplateKeywords lists the placeholders valid in
// label_directory_template.
func LabelTemplateKeywords() []string {
	return keywordsOf((&Label{}).templateValues())
}

// ArtistTemplateKeywords lists the placeholders valid in
// artist_directory_template.
func ArtistTemplateKeywords() []string {
	return keywordsOf((&Artist{}).templateValues())
}
