package services

import (
	"os"
	"path/filepath"
)

// sidecarExe is os.Executable, als variabele zodat tests een andere plek kunnen
// voorwenden.
var sidecarExe = os.Executable

// vindSidecar zoekt een sidecar-script op de plekken waar het kan staan.
//
// Het pad mag niet aan de werkmap hangen: een .app die vanuit Finder wordt geopend
// heeft "/" als werkmap, en dan wordt "sidecar/pdf.mjs" opeens "/sidecar/pdf.mjs".
// Dat is precies de fout die je krijgt zodra de tool niet vanuit de projectmap start.
// Daarom eerst naast het programma zelf kijken, en de werkmap als laatste.
//
// Zoekorde:
//  1. de omgevingsvariabele (handmatige override, ook handig in tests)
//  2. <app>.app/Contents/Resources/sidecar/<naam> — een gebouwd macOS-bundel
//  3. <map van het binary>/sidecar/<naam> — binary met de sidecar ernaast
//  4. <map van het binary>/../sidecar/<naam> — repo-indeling: bin/<binary>
//  5. sidecar/<naam> in de werkmap — de dev-server
//
// Wordt niets gevonden, dan komt het werkmap-pad terug: dan noemt de foutmelding een
// pad dat een mens herkent.
func vindSidecar(naam, envVar string) string {
	if p := os.Getenv(envVar); p != "" {
		return p
	}

	werkmap := filepath.Join("sidecar", naam)
	kandidaten := make([]string, 0, 4)
	if exe, err := sidecarExe(); err == nil {
		dir := filepath.Dir(exe)
		kandidaten = append(kandidaten,
			filepath.Join(dir, "..", "Resources", "sidecar", naam),
			filepath.Join(dir, "sidecar", naam),
			filepath.Join(dir, "..", "sidecar", naam),
		)
	}
	kandidaten = append(kandidaten, werkmap)

	for _, k := range kandidaten {
		if st, err := os.Stat(k); err == nil && !st.IsDir() {
			return filepath.Clean(k)
		}
	}
	return werkmap
}
