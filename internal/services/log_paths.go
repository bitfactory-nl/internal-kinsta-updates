package services

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Een logregel noemt een productiepad, en dat moet op een bestand in de checkout
// uitkomen voordat we er een AI op los laten. Kinsta is daarin niet consequent:
// in één en hetzelfde error.log staan
//
//	/www/voorbeeld_706/public/wp-includes/css/
//	/www/voorbeeld_706/web/public/wp-settings.php
//
// terwijl de repo lokaal `public/…` heeft (en sommige projecten `web/…`). De
// mapping herankert daarom op de webroot in plaats van op een vast prefix, en
// controleert altijd of het bestand er écht staat.

// lokaleWebroots zijn de plekken waar de webroot in een checkout kan zitten, in
// de volgorde waarin we ze proberen.
var lokaleWebroots = []string{"public", "web/public", "web", "."}

// reWWWPad matcht het Kinsta-patroon /www/<site>/<rest>.
var reWWWPad = regexp.MustCompile(`^/www/[^/]+/(.+)$`)

// reCoreBestandInWebroot matcht de losse core-bestanden in de webroot zelf
// (wp-settings.php, wp-load.php, wp-config.php, xmlrpc.php, …).
var reCoreBestandInWebroot = regexp.MustCompile(`^(?:wp-[a-z0-9-]+\.php|xmlrpc\.php|index\.php)$`)

// RepoBestand is de uitkomst van het mappen van een productiepad.
type RepoBestand struct {
	// RepoPad is relatief aan de repo-root, met forward slashes. Leeg als het
	// bestand niet gevonden is.
	RepoPad string
	// WebrootPad is het pad vanaf de webroot, bv wp-content/themes/x/f.php.
	WebrootPad string
	// Bestaat is true als het bestand daadwerkelijk in de checkout staat.
	Bestaat bool
	// IsCore markeert WordPress core: daar hoort geen fix in thuis, want een
	// core-update overschrijft het weer.
	IsCore bool
}

// webrootRelatief haalt uit een productiepad het deel vanaf de webroot.
func webrootRelatief(prodPad string) string {
	prodPad = strings.TrimSpace(prodPad)
	if prodPad == "" {
		return ""
	}
	prodPad = filepath.ToSlash(prodPad)

	// De laatste /public/ is de webroot: bij /www/x/web/public/ is dat de
	// tweede, bij /www/x/public/ de enige.
	if i := strings.LastIndex(prodPad, "/public/"); i >= 0 {
		return strings.TrimPrefix(prodPad[i+len("/public/"):], "/")
	}
	// Zonder /public/ ankeren we op een map die altijd in de webroot zit.
	for _, anker := range []string{"/wp-content/", "/wp-includes/", "/wp-admin/"} {
		if i := strings.LastIndex(prodPad, anker); i >= 0 {
			return strings.TrimPrefix(prodPad[i+1:], "/")
		}
	}
	if m := reWWWPad.FindStringSubmatch(prodPad); m != nil {
		return m[1]
	}
	return ""
}

// isCoreWebrootPad reports whether a webroot-relative path belongs to WordPress
// core rather than to the project's own code.
func isCoreWebrootPad(webrootPad string) bool {
	if webrootPad == "" {
		return false
	}
	if strings.HasPrefix(webrootPad, "wp-includes/") || strings.HasPrefix(webrootPad, "wp-admin/") {
		return true
	}
	return reCoreBestandInWebroot.MatchString(webrootPad)
}

// mapProdPathToRepo maps a production path from a log line onto a file in the
// checkout. Het pad uit het log is onvertrouwde invoer, dus het resultaat wordt
// altijd getoetst: buiten de repo uitkomen levert geen treffer op.
func mapProdPathToRepo(prodPad, repoRoot string) RepoBestand {
	webPad := webrootRelatief(prodPad)
	if webPad == "" || repoRoot == "" {
		return RepoBestand{}
	}
	uit := RepoBestand{WebrootPad: webPad, IsCore: isCoreWebrootPad(webPad)}

	wortel, err := filepath.Abs(repoRoot)
	if err != nil {
		return uit
	}
	for _, webroot := range lokaleWebroots {
		kandidaat := filepath.Join(wortel, filepath.FromSlash(webroot), filepath.FromSlash(webPad))
		if !binnenWortel(wortel, kandidaat) {
			// Een pad met .. erin mag ons niet buiten de checkout brengen.
			continue
		}
		info, err := os.Stat(kandidaat)
		if err != nil || info.IsDir() {
			continue
		}
		rel, err := filepath.Rel(wortel, kandidaat)
		if err != nil {
			continue
		}
		uit.RepoPad = filepath.ToSlash(rel)
		uit.Bestaat = true
		return uit
	}
	return uit
}

// binnenWortel checks that pad stays inside wortel.
func binnenWortel(wortel, pad string) bool {
	rel, err := filepath.Rel(wortel, filepath.Clean(pad))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
