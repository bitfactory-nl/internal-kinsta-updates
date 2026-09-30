package services

import (
	"testing"

	"github.com/rdm/sites-tool/internal/adapters/kinsta"
)

func TestSuggestKinstaSite(t *testing.T) {
	sites := []kinsta.Site{{ID: "1", Name: "ekcoating", DisplayName: "EK Coating"}, {ID: "2", Name: "satispress", DisplayName: "SatisPress"}}
	for url, want := range map[string]string{
		"https://www.ekcoating.nl":        "1",
		"https://satispress.nobears.dev/": "2",
		"https://onbekend.nl":             "",
	} {
		got := suggestKinstaSite(sites, url)
		if got.SiteID != want || got.Found != (want != "") {
			t.Errorf("%s: %+v, want %q", url, got, want)
		}
	}
	// Twee treffers -> geen voorstel.
	dubbel := append(sites, kinsta.Site{ID: "3", Name: "ek-coating"})
	if got := suggestKinstaSite(dubbel, "https://ekcoating.nl"); got.Found {
		t.Fatalf("dubbelzinnig: %+v", got)
	}
}
