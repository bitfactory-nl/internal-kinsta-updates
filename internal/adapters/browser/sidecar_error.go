package browser

import (
	"fmt"
	"strings"
)

// verduidelijk zet een ruwe Node-fout om in een melding waar iemand iets mee kan.
// De onbewerkte varianten hiervan zijn een stacktrace van twintig regels waaruit
// niet blijkt wát er moet gebeuren; de drie gevallen hieronder zijn precies de
// dingen die op een verse machine ontbreken.
func verduidelijk(script string, err error, uitvoer string) error {
	if err == nil {
		return nil
	}
	alles := err.Error() + " " + uitvoer

	switch {
	case strings.Contains(alles, "Cannot find module"), strings.Contains(alles, "MODULE_NOT_FOUND"):
		return fmt.Errorf("het sidecar-script is niet gevonden op %q. "+
			"Bij een gebouwde app hoort het in Contents/Resources/sidecar (opnieuw bouwen lost dit op); "+
			"start je vanuit de broncode, doe dat dan vanuit de projectmap of zet RDM_SIDECAR op het volledige pad", script)

	case strings.Contains(alles, "Executable doesn't exist"),
		strings.Contains(alles, "playwright install"),
		strings.Contains(alles, "browserType.launch"):
		return fmt.Errorf("de browser van Playwright is niet geïnstalleerd op deze machine. "+
			"Draai eenmalig: cd sidecar && npx playwright install chromium (oorspronkelijke fout: %w)", err)

	case strings.Contains(alles, "executable file not found"), strings.Contains(alles, "\"node\": executable"):
		return fmt.Errorf("Node.js is niet gevonden. Installeer Node 20 of nieuwer en start de tool opnieuw (oorspronkelijke fout: %w)", err)
	}
	return err
}
