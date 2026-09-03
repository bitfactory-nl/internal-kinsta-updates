package services

import (
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
)

// changelogKoppen verbindt de subkoppen uit de release-notes met de soorten uit
// het domeinmodel. De sleutels staan in kleine letters; de vergelijking is
// hoofdletterongevoelig.
var changelogKoppen = map[string]string{
	"nieuw":    domain.ChangeNieuw,
	"opgelost": domain.ChangeOpgelost,
	"overig":   domain.ChangeOverig,
}

// parseChangelog haalt de "## Wijzigingen"-sectie uit een release-body en zet
// de bullets om in ChangeEntry's, gegroepeerd op de subkop waaronder ze staan.
// Regels vóór de eerste subkop tellen als "overig". Ontbreekt de sectie — zoals
// in alle releases tot en met v0.2.9, die alleen installatie-instructies
// bevatten — dan is het resultaat leeg en meldt de UI dat er geen details zijn.
func parseChangelog(body string) []domain.ChangeEntry {
	var (
		entries  []domain.ChangeEntry
		inSectie bool
		kind     = domain.ChangeOverig
	)

	for _, ruw := range strings.Split(body, "\n") {
		regel := strings.TrimSpace(strings.TrimSuffix(ruw, "\r"))

		if strings.HasPrefix(regel, "## ") {
			// Een nieuwe H2 opent de sectie, of sluit hem weer (bijvoorbeeld
			// "## Installatie" dat op de wijzigingen volgt).
			inSectie = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(regel, "## ")), "wijzigingen")
			kind = domain.ChangeOverig
			continue
		}
		if !inSectie {
			continue
		}

		if strings.HasPrefix(regel, "### ") {
			kop := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(regel, "### ")))
			if k, ok := changelogKoppen[kop]; ok {
				kind = k
			} else {
				kind = domain.ChangeOverig
			}
			continue
		}

		if !strings.HasPrefix(regel, "- ") && !strings.HasPrefix(regel, "* ") {
			continue
		}
		tekst := strings.TrimSpace(regel[2:])
		if tekst == "" {
			continue
		}
		entries = append(entries, domain.ChangeEntry{Kind: kind, Text: tekst})
	}

	return entries
}
