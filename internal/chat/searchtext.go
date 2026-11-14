package chat

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// SearchText is the text a message is indexed by, and query terms are matched
// with (add-native-message-search design D1): lower case, accents folded,
// everything that is not a letter or a number (Markdown syntax, punctuation)
// a space, words separated by one space. The databases' own analyzers do
// nothing more (PostgreSQL 'simple', FTS5 unicode61 without diacritics folding,
// MariaDB's FULLTEXT), so the three agree.
func SearchText(s string) string {
	// a new chain per call: transformers keep state
	folded, _, err := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), s)
	if err != nil {
		folded = s
	}
	var b strings.Builder
	b.Grow(len(folded))
	space := true
	for _, r := range strings.ToLower(folded) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			space = false
			continue
		}
		if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSuffix(b.String(), " ")
}

// searchWords splits a query term into the words of its search text.
func searchWords(term string) []string { return strings.Fields(SearchText(term)) }
