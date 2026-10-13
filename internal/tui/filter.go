package tui

import (
	"strings"
	"unicode"
)

// Match a case-insensitive subsequence, rewarding adjacent letters and word
// boundaries. Each field is matched independently so a pod name cannot borrow
// unrelated restart/node digits to satisfy the search.
func fuzzyScore(query, value string) (int, bool) {
	needle, hay := []rune(strings.ToLower(query)), []rune(strings.ToLower(value))
	if len(needle) == 0 {
		return 0, true
	}
	score, pos, last := 0, 0, -2
	for _, want := range needle {
		found := -1
		for i := pos; i < len(hay); i++ {
			if hay[i] == want {
				found = i
				break
			}
		}
		if found < 0 {
			return 0, false
		}
		score += 20
		if found == last+1 {
			score += 15
		}
		if found == 0 || !unicode.IsLetter(hay[found-1]) && !unicode.IsDigit(hay[found-1]) {
			score += 10
		}
		score -= min(10, found-pos)
		last, pos = found, found+1
	}
	return score, true
}
