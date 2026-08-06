package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// De vangrails staan hier apart van de orkestratie omdat ze het verschil maken
// tussen "een AI mag code aanpassen" en "een AI mag ongezien naar GitHub
// pushen". Ze zijn puur, zodat een test ze los kan aanroepen.

// gewijzigdeBestanden leest de uitvoer van `git status --porcelain`.
//
// Het formaat is XY<spatie>pad, met bij een rename `oud -> nieuw`, en paden met
// bijzondere tekens tussen dubbele quotes.
func gewijzigdeBestanden(porcelain string) []string {
	var uit []string
	for _, regel := range strings.Split(porcelain, "\n") {
		if len(regel) < 4 {
			continue
		}
		pad := strings.TrimSpace(regel[2:])
		// Bij een rename is alleen het nieuwe pad interessant.
		if i := strings.Index(pad, " -> "); i >= 0 {
			pad = pad[i+len(" -> "):]
		}
		pad = strings.Trim(pad, `"`)
		if pad = strings.TrimSpace(pad); pad != "" {
			uit = append(uit, filepath.ToSlash(pad))
		}
	}
	return uit
}

// repoPadIsCore reports whether a repo-relative path is a WordPress core file.
// De lokale webroot kan public/, web/public/ of web/ zijn, dus het prefix wordt
// er eerst afgehaald.
func repoPadIsCore(repoPad string) bool {
	repoPad = filepath.ToSlash(strings.TrimSpace(repoPad))
	// Langste webroot eerst, anders vangt "web/" al "web/public/…" af.
	for _, webroot := range []string{"web/public", "public", "web"} {
		if rest, ok := strings.CutPrefix(repoPad, webroot+"/"); ok {
			return isCoreWebrootPad(rest)
		}
	}
	return isCoreWebrootPad(repoPad)
}

// controleerGewijzigdePaden is de harde grens op wat een AI-run mag hebben
// aangeraakt. Alles wat hier faalt betekent: niet committen, niet pushen.
func controleerGewijzigdePaden(bestanden []string) error {
	if len(bestanden) == 0 {
		return fmt.Errorf("de AI heeft geen enkel bestand gewijzigd")
	}
	var core, buiten, verboden []string
	for _, b := range bestanden {
		switch {
		case strings.HasPrefix(b, "../") || strings.Contains(b, "/../") || filepath.IsAbs(b):
			buiten = append(buiten, b)
		case repoPadIsCore(b):
			core = append(core, b)
		case b == "composer.json" || b == "composer.lock" || b == "package.json" || b == "package-lock.json" || b == "yarn.lock":
			verboden = append(verboden, b)
		}
	}
	if len(buiten) > 0 {
		return fmt.Errorf("er is buiten de checkout geschreven: %s", strings.Join(buiten, ", "))
	}
	if len(core) > 0 {
		return fmt.Errorf("er is in WordPress core gewijzigd (%s); dat wordt bij de volgende core-update overschreven, dus dit gaat niet naar GitHub", strings.Join(core, ", "))
	}
	if len(verboden) > 0 {
		return fmt.Errorf("de afhankelijkheden zijn aangepast (%s); dat hoort niet bij het oplossen van een logfout", strings.Join(verboden, ", "))
	}
	return nil
}

// phpBestanden filters the PHP files from a change list.
func phpBestanden(bestanden []string) []string {
	var uit []string
	for _, b := range bestanden {
		if strings.EqualFold(filepath.Ext(b), ".php") {
			uit = append(uit, b)
		}
	}
	return uit
}

// Test seams voor de lint-stap.
var (
	phpLookPath    = exec.LookPath
	phpExecCommand = exec.CommandContext
	phpVastePaden  = []string{
		"/opt/homebrew/bin/php",
		"/usr/local/bin/php",
		"/usr/bin/php",
	}
	phpEenmalig sync.Once
	phpGevonden string
)

// PhpBin zoekt de php-binary. Herd (de lokale PHP-omgeving van deze
// werkplekken) zet php in Application Support en niet op het systeem-PATH van
// een .app, dus die plek staat er expliciet bij.
func PhpBin() string {
	phpEenmalig.Do(func() { phpGevonden = zoekPHP() })
	return phpGevonden
}

func zoekPHP() string {
	if p := strings.TrimSpace(os.Getenv("RDM_PHP")); p != "" {
		return p
	}
	if p, err := phpLookPath("php"); err == nil && p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		if p := filepath.Join(home, "Library", "Application Support", "Herd", "bin", "php"); uitvoerbaarBestand(p) {
			return p
		}
	}
	for _, kandidaat := range phpVastePaden {
		if uitvoerbaarBestand(kandidaat) {
			return kandidaat
		}
	}
	return ""
}

func uitvoerbaarBestand(pad string) bool {
	st, err := os.Stat(pad)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// lintPHP draait `php -l` op elk gewijzigd PHP-bestand. Zonder php op de machine
// wordt er niet stil overgeslagen: de uitkomst zegt dan expliciet dat er niet
// gecontroleerd is, want dat is iets anders dan "goedgekeurd".
func lintPHP(ctx context.Context, worktree string, bestanden []string) (uitvoer string, err error) {
	php := PhpBin()
	if php == "" {
		return "php niet gevonden op deze machine, dus de syntaxcontrole is niet gedraaid (zet het pad in RDM_PHP)", nil
	}
	var b strings.Builder
	var kapot []string
	for _, bestand := range bestanden {
		volledig := filepath.Join(worktree, filepath.FromSlash(bestand))
		if !binnenWortel(worktree, volledig) {
			return b.String(), fmt.Errorf("pad %q valt buiten de worktree", bestand)
		}
		out, lintErr := phpExecCommand(ctx, php, "-l", volledig).CombinedOutput()
		regel := strings.TrimSpace(string(out))
		fmt.Fprintf(&b, "%s: %s\n", bestand, regel)
		if lintErr != nil {
			kapot = append(kapot, bestand)
		}
	}
	if len(kapot) > 0 {
		return b.String(), fmt.Errorf("php -l faalt op %s", strings.Join(kapot, ", "))
	}
	return b.String(), nil
}
