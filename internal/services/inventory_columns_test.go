package services

import "testing"

func TestBuildProjectRefKolommen(t *testing.T) {
	tests := []struct {
		naam            string
		lokaal, github  string
		latest          string
		wantOutdated    bool
		wantLocalBehind bool
	}{
		{
			naam:   "github verouderd, lokaal gelijk",
			lokaal: "6.7.2", github: "6.7.2", latest: "7.0.2",
			wantOutdated: true, wantLocalBehind: false,
		},
		{
			naam:   "github actueel, lokale checkout loopt achter",
			lokaal: "6.7.2", github: "7.0.2", latest: "7.0.2",
			wantOutdated: false, wantLocalBehind: true,
		},
		{
			naam:   "beide actueel",
			lokaal: "7.0.2", github: "7.0.2", latest: "7.0.2",
			wantOutdated: false, wantLocalBehind: false,
		},
		{
			naam:   "lokaal vooruit op github (eigen werk nog niet gepusht)",
			lokaal: "7.0.2", github: "6.7.2", latest: "7.0.2",
			wantOutdated: true, wantLocalBehind: false,
		},
		{
			naam:   "onbekende laatste versie: nooit verouderd",
			lokaal: "6.7.2", github: "6.7.2", latest: "",
			wantOutdated: false, wantLocalBehind: false,
		},
		{
			naam:   "geen lokale versie gelezen: geen achterloop-markering",
			lokaal: "", github: "7.0.2", latest: "7.0.2",
			wantOutdated: false, wantLocalBehind: false,
		},
		{
			naam:   "geen github-kolom: verouderd valt terug op lokaal",
			lokaal: "6.7.2", github: "", latest: "7.0.2",
			wantOutdated: true, wantLocalBehind: false,
		},
	}
	for _, tt := range tests {
		got := buildProjectRef("p1", "Project", tt.lokaal, tt.github, tt.latest, "origin/release/1.0.x")
		if got.Outdated != tt.wantOutdated {
			t.Errorf("%s: Outdated = %v, want %v", tt.naam, got.Outdated, tt.wantOutdated)
		}
		if got.LocalBehind != tt.wantLocalBehind {
			t.Errorf("%s: LocalBehind = %v, want %v", tt.naam, got.LocalBehind, tt.wantLocalBehind)
		}
		if got.LocalVersion != tt.lokaal || got.GithubVersion != tt.github {
			t.Errorf("%s: kolommen = %q/%q, want %q/%q", tt.naam, got.LocalVersion, got.GithubVersion, tt.lokaal, tt.github)
		}
	}
}
