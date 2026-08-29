// Package importer — see importer.go. This file covers standardizing the
// video file name after confirmation (Story 3, additional criterion
// recorded in docs/phase-1-mvp.md and designed in
// docs/superpowers/specs/2026-07-23-story-3-standardized-filename-design.md).
package importer

import (
	"fmt"
	"regexp"
	"strings"
)

// accentReplacer strips the most common accents in PT/ES proper names —
// avoids depending on a transliteration lib just for this (CLAUDE.md: no
// new dependency without justification). The input is expected to already be
// lowercase (strings.ToLower already normalizes most of the corresponding
// uppercase/accented forms).
var accentReplacer = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n", "ý", "y",
)

var nonSlugRun = regexp.MustCompile(`[^a-z0-9]+`)

// slugify normalizes a tutor name for use in a file name: lowercase,
// no accents, any run of characters outside [a-z0-9] becomes a
// single "-", with no "-" at the ends.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = accentReplacer.Replace(s)
	s = nonSlugRun.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// StandardFilename derives the standardized file name after confirmation, in the
// format "YYYY-MM-DD_HHHMM_tutor-slug.ext" (e.g. "2026-07-23_14H30_maria-jose.mp4").
// lessonDate is the raw value of <input type="datetime-local">
// ("YYYY-MM-DDTHH:MM"); the caller (services/import.go ConfirmImport) already
// guarantees this format before calling this function — there's no fallback here
// for a date without a time. ext includes the dot (e.g. ".mp4"), as returned
// by filepath.Ext, and is normalized to lowercase.
func StandardFilename(lessonDate, tutor, ext string) string {
	datePart, timePart, _ := strings.Cut(lessonDate, "T")
	timePart = strings.ReplaceAll(timePart, ":", "H")
	slug := slugify(tutor)
	if slug == "" {
		// Tutor with no alphanumeric character at all (e.g. "..."). ConfirmImport only
		// rejects an empty tutor, not this degenerate case — falls here to never
		// produce a file name with a stray "_" before the extension.
		slug = "tutor"
	}
	return fmt.Sprintf("%s_%s_%s%s", datePart, timePart, slug, strings.ToLower(ext))
}
