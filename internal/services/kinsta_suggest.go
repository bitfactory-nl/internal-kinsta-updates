package services

import (
	"net/url"
	"strings"
	"unicode"

	"github.com/rdm/sites-tool/internal/adapters/kinsta"
)

// KinstaSuggestion is het voorstel voor een Kinsta-site bij een site-URL.
// Found is alleen true bij precies één treffer; bij twijfel geen voorstel.
type KinstaSuggestion struct {
	SiteID string `json:"siteId"`
	Name   string `json:"name"`
	Found  bool   `json:"found"`
}

// SuggestSiteForURL zoekt in de Kinsta-sites naar de site die bij de URL hoort.
func (s *KinstaService) SuggestSiteForURL(url string) (KinstaSuggestion, error) {
	sites, err := s.ListSites()
	if err != nil {
		return KinstaSuggestion{}, err
	}
	return suggestKinstaSite(sites, url), nil
}

// suggestKinstaSite vergelijkt het eerste hostlabel van de URL (zonder poort
// en zonder "www.") met de genormaliseerde Name en DisplayName van elke site.
func suggestKinstaSite(sites []kinsta.Site, rawURL string) KinstaSuggestion {
	label := hostLabel(rawURL)
	if label == "" {
		return KinstaSuggestion{}
	}
	var hit *kinsta.Site
	for i := range sites {
		if normaliseerKinstaNaam(sites[i].Name) == label || normaliseerKinstaNaam(sites[i].DisplayName) == label {
			if hit != nil {
				return KinstaSuggestion{}
			}
			hit = &sites[i]
		}
	}
	if hit == nil {
		return KinstaSuggestion{}
	}
	name := hit.DisplayName
	if name == "" {
		name = hit.Name
	}
	return KinstaSuggestion{SiteID: hit.ID, Name: name, Found: true}
}

func hostLabel(rawURL string) string {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	label, _, _ := strings.Cut(host, ".")
	return label
}

// normaliseerKinstaNaam maakt een naam lowercase en houdt alleen letters en
// cijfers over ("ek-coating" en "EK Coating" worden "ekcoating").
func normaliseerKinstaNaam(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
