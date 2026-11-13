// Package theme is the registry of MeetHub's visual themes: the source of
// truth for the API, preference validation and the SPA's lockstep test.
package theme

// Default is the theme used when nothing else is chosen.
const Default = "meethub-dark"

// Theme is one entry of the catalog.
type Theme struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Scheme  string `json:"scheme"` // "light" or "dark"
	Default bool   `json:"default,omitempty"`
}

var themes = []Theme{
	{ID: "meethub-dark", Label: "MeetHub (dark)", Scheme: "dark", Default: true},
	{ID: "meethub-light", Label: "MeetHub (light)", Scheme: "light"},
	{ID: "default", Label: "Default", Scheme: "light"},
	{ID: "admanager", Label: "ADManager", Scheme: "light"},
	{ID: "breeze", Label: "Breeze", Scheme: "light"},
	{ID: "breeze-dark", Label: "Breeze (dark)", Scheme: "dark"},
	{ID: "violet", Label: "Violet", Scheme: "light"},
	{ID: "violet-dark", Label: "Violet (dark)", Scheme: "dark"},
	{ID: "aurora", Label: "Aurora", Scheme: "light"},
	{ID: "aurora-dark", Label: "Aurora (dark)", Scheme: "dark"},
	{ID: "crystal", Label: "Crystal", Scheme: "light"},
	{ID: "crystal-dark", Label: "Crystal (dark)", Scheme: "dark"},
	{ID: "cone", Label: "Cone (dark)", Scheme: "dark"},
	{ID: "classic-blue", Label: "Classic blue (XP)", Scheme: "light"},
	{ID: "classic-gray", Label: "Classic gray (98)", Scheme: "light"},
	{ID: "graphite", Label: "Graphite (dark)", Scheme: "dark"},
	{ID: "grafana", Label: "Grafana (dark)", Scheme: "dark"},
	{ID: "rabbitmq", Label: "RabbitMQ", Scheme: "light"},
	{ID: "matrix", Label: "Matrix (dark)", Scheme: "dark"},
	{ID: "winamp", Label: "Winamp (dark)", Scheme: "dark"},
	{ID: "pink-panther", Label: "Pink Panther", Scheme: "light"},
	{ID: "high-contrast", Label: "High contrast (dark)", Scheme: "dark"},
}

// All returns a copy of the catalog in display order.
func All() []Theme { return append([]Theme(nil), themes...) }

// Lookup returns the theme with id.
func Lookup(id string) (Theme, bool) {
	for _, t := range themes {
		if t.ID == id {
			return t, true
		}
	}
	return Theme{}, false
}

// Valid reports whether id names a theme.
func Valid(id string) bool { _, ok := Lookup(id); return ok }
