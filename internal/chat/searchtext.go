package chat

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// searchWordMax is the longest word kept (longer ones are cut): under MariaDB's
// FULLTEXT maximum (84 characters) and PostgreSQL's 2 KB per word, so every word
// of a message is in the index and its beginning can be found.
const searchWordMax = 80

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
	space, n := true, 0
	for _, r := range strings.ToLower(folded) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			if n < searchWordMax {
				b.WriteRune(r)
			}
			space = false
			n++
			continue
		}
		if !space {
			b.WriteByte(' ')
			space, n = true, 0
		}
	}
	return strings.TrimSuffix(b.String(), " ")
}

// searchWords splits a query term into the words of its search text.
func searchWords(term string) []string { return strings.Fields(SearchText(term)) }
