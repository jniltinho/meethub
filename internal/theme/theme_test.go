package theme_test

import (
	"testing"

	"github.com/jniltinho/meethub/internal/theme"
)

func TestCatalog(t *testing.T) {
	t.Parallel()
	all := theme.All()
	if len(all) != 22 {
		t.Fatalf("%d themes, want 22", len(all))
	}
	seen, defaults := map[string]bool{}, 0
	for _, th := range all {
		if seen[th.ID] {
			t.Errorf("duplicate id %q", th.ID)
		}
		seen[th.ID] = true
		if th.Scheme != "light" && th.Scheme != "dark" {
			t.Errorf("%s: scheme %q", th.ID, th.Scheme)
		}
		if th.Default {
			defaults++
		}
	}
	if defaults != 1 || !all[0].Default || all[0].ID != theme.Default {
		t.Fatal("meethub-dark must be the single default")
	}
	if !theme.Valid("classic-blue") || theme.Valid("neon") {
		t.Fatal("Valid")
	}
}
