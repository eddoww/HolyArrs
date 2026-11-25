package releasename

import (
	"slices"
	"strconv"
	"strings"
)

// The fork reads movie names as Radarr does: a year joined to the words
// around it by hyphens, an edition before the year, and a leading
// [bracket] (Parse).

// movieTokens splits a movie name into its words.
func movieTokens(s string) []string {
	toks := reSeparators.Split(strings.TrimSpace(s), -1)
	toks = slices.DeleteFunc(toks, func(t string) bool { return t == "" })
	return splitTagPairs(splitJoinedYears(toks))
}

// splitJoinedYears splits a year joined to the words around it by hyphens
// ("Romulus-2024-1080p", "2024-COMPLETE") when a tag follows the year;
// other words stay as they are, so "Spider-Man" and "2002-2017" are kept.
func splitJoinedYears(toks []string) []string {
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		parts := strings.Split(t, "-")
		at := -1
		for k := 0; k+1 < len(parts); k++ {
			if reYear.MatchString(parts[k]) && isTag(parts[k+1]) {
				at = k
				break
			}
		}
		if at < 0 || slices.Contains(parts, "") {
			out = append(out, t)
			continue
		}
		if at > 0 {
			out = append(out, strings.Join(parts[:at], "-"))
		}
		out = append(out, parts[at], strings.Join(parts[at+1:], "-"))
	}
	return out
}

// isYear is a release year: 1900 to next year.
func isYear(tok string) bool {
	if !reYear.MatchString(tok) {
		return false
	}
	y, _ := strconv.Atoi(tok)
	return y <= nextYear
}

// editionBeforeYear finds an edition that stands between the title and
// the year: first is the first tag, and it must be an edition word. It
// gives where the edition starts (an ordinal before it belongs to it) and
// where the year is, or at -1 when no year follows the edition.
func editionBeforeYear(toks []string, first int) (start, at int) {
	if first >= len(toks) || !editionRunWords[strings.ToLower(toks[first])] {
		return 0, -1
	}
	start = first
	if start > 1 && reOrdinal.MatchString(toks[start-1]) {
		start--
	}
	j := first + 1
	for j < len(toks) && editionRunWords[strings.ToLower(toks[j])] {
		j++
	}
	if start < 1 || j >= len(toks) || !isYear(toks[j]) {
		return 0, -1
	}
	return start, j
}
