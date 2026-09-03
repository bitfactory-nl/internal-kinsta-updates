package services

import (
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestParseChangelogGroepeertPerKop(t *testing.T) {
	body := "## Wijzigingen\n" +
		"\n" +
		"### Nieuw\n" +
		"- Zelf-update van de app\n" +
		"- Badge in de sidebar\n" +
		"\n" +
		"### Opgelost\n" +
		"- Versie bleef op de oude waarde staan\n" +
		"\n" +
		"### Overig\n" +
		"- Afhankelijkheden bijgewerkt\n" +
		"\n" +
		"## Installatie\n" +
		"\n" +
		"1. Download de zip\n"

	got := parseChangelog(body)

	wil := []domain.ChangeEntry{
		{Kind: domain.ChangeNieuw, Text: "Zelf-update van de app"},
		{Kind: domain.ChangeNieuw, Text: "Badge in de sidebar"},
		{Kind: domain.ChangeOpgelost, Text: "Versie bleef op de oude waarde staan"},
		{Kind: domain.ChangeOverig, Text: "Afhankelijkheden bijgewerkt"},
	}
	if len(got) != len(wil) {
		t.Fatalf("aantal regels = %d, wil %d (%+v)", len(got), len(wil), got)
	}
	for i := range wil {
		if got[i] != wil[i] {
			t.Errorf("regel %d = %+v, wil %+v", i, got[i], wil[i])
		}
	}
}

func TestParseChangelogStoptBijDeVolgendeH2(t *testing.T) {
	body := "## Wijzigingen\n\n### Nieuw\n- Eerste\n\n## Installatie\n\n### Nieuw\n- Niet meenemen\n"

	got := parseChangelog(body)

	if len(got) != 1 || got[0].Text != "Eerste" {
		t.Errorf("regels = %+v, wil alleen \"Eerste\"", got)
	}
}

func TestParseChangelogZonderWijzigingenSectie(t *testing.T) {
	// Alle releases tot en met v0.2.9 hebben alleen installatie-instructies.
	body := "## Installatie\n\n1. Download `RDM-Sites-Tool-v0.2.9-macOS.zip`\n2. Pak uit\n"

	if got := parseChangelog(body); len(got) != 0 {
		t.Errorf("regels = %+v, wil leeg", got)
	}
}

func TestParseChangelogLegeBody(t *testing.T) {
	if got := parseChangelog(""); len(got) != 0 {
		t.Errorf("regels = %+v, wil leeg", got)
	}
}

func TestParseChangelogRegelsZonderKopWordenOverig(t *testing.T) {
	body := "## Wijzigingen\n\n- Losse regel zonder subkop\n"

	got := parseChangelog(body)

	if len(got) != 1 || got[0].Kind != domain.ChangeOverig || got[0].Text != "Losse regel zonder subkop" {
		t.Errorf("regels = %+v, wil één overig-regel", got)
	}
}

func TestParseChangelogNegeertLegeBulletsEnSterretjes(t *testing.T) {
	body := "## Wijzigingen\n\n### Nieuw\n* Met een sterretje\n-\n-    \n- Met streepje\n"

	got := parseChangelog(body)

	if len(got) != 2 {
		t.Fatalf("regels = %+v, wil 2", got)
	}
	if got[0].Text != "Met een sterretje" || got[1].Text != "Met streepje" {
		t.Errorf("regels = %+v", got)
	}
}

func TestParseChangelogIsNietGevoeligVoorHoofdlettersEnCRLF(t *testing.T) {
	body := "## wijzigingen\r\n\r\n### NIEUW\r\n- Werkt ook zo\r\n"

	got := parseChangelog(body)

	if len(got) != 1 || got[0].Kind != domain.ChangeNieuw || got[0].Text != "Werkt ook zo" {
		t.Errorf("regels = %+v, wil één nieuw-regel", got)
	}
}
