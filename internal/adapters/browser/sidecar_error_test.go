package browser

import (
	"errors"
	"strings"
	"testing"
)

func TestVerduidelijk(t *testing.T) {
	gevallen := []struct {
		naam    string
		err     error
		uitvoer string
		wil     string
	}{
		{
			naam: "ontbrekend script",
			err:  errors.New("sidecar exec: exit status 1"),
			// Precies de melding die een collega kreeg met een gebouwde app.
			uitvoer: "Error: Cannot find module '/sidecar/pdf.mjs'\n    code: 'MODULE_NOT_FOUND'",
			wil:     "Contents/Resources/sidecar",
		},
		{
			naam:    "browser niet geïnstalleerd",
			err:     errors.New("sidecar exec: exit status 1"),
			uitvoer: "browserType.launch: Executable doesn't exist at /Users/x/Library/Caches/ms-playwright/chromium-1140",
			wil:     "npx playwright install chromium",
		},
		{
			naam:    "node ontbreekt",
			err:     errors.New(`exec: "node": executable file not found in $PATH`),
			uitvoer: "",
			wil:     "Node.js is niet gevonden",
		},
	}
	for _, g := range gevallen {
		t.Run(g.naam, func(t *testing.T) {
			got := verduidelijk("sidecar/pdf.mjs", g.err, g.uitvoer)
			if got == nil || !strings.Contains(got.Error(), g.wil) {
				t.Errorf("melding = %v; wil iets over %q", got, g.wil)
			}
		})
	}

	// Een fout die we niet herkennen moet ongewijzigd doorgaan.
	origineel := errors.New("iets heel anders")
	if got := verduidelijk("s", origineel, ""); got != origineel {
		t.Errorf("onbekende fout is aangepast: %v", got)
	}
	if verduidelijk("s", nil, "") != nil {
		t.Error("nil hoort nil te blijven")
	}
}
