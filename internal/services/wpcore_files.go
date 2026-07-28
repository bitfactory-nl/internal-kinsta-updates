package services

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// wpRootDir is de WordPress-webroot binnen een project (zelfde conventie als
// wpplugins.PluginsDir).
func wpRootDir(projectPath string) string {
	return filepath.Join(projectPath, "public")
}

// coreDirs zijn de mappen die volledig bij WordPress core horen en dus in hun
// geheel vervangen worden. wp-content valt hier bewust niet onder: daar staan
// thema's, plugins en uploads van de klant.
var coreDirs = []string{"wp-admin", "wp-includes"}

// coreRootExtras zijn core-bestanden in de webroot die niet op "wp-" beginnen.
var coreRootExtras = map[string]bool{
	"index.php":   true,
	"xmlrpc.php":  true,
	"readme.html": true,
	"license.txt": true,
}

// isCoreRootFile meldt of een bestand in de webroot bij WordPress core hoort en
// dus vervangen mag worden. wp-config.php bevat de projectconfiguratie (o.a.
// databasegegevens) en wordt nooit aangeraakt; bestanden van het project zelf
// matchen geen enkel patroon en blijven daardoor automatisch staan.
func isCoreRootFile(naam string) bool {
	if naam == "wp-config.php" {
		return false
	}
	if coreRootExtras[naam] {
		return true
	}
	return strings.HasPrefix(naam, "wp-") && strings.HasSuffix(naam, ".php")
}

// replaceCore vervangt de WordPress core in wpRoot door de inhoud van een
// "no-content" core-zip (entries onder wordpress/, zonder wp-content).
//
// Werkwijze: eerst de zip volledig valideren en in geheugen inlezen, daarna de
// oude core-mappen en core-rootbestanden verwijderen en de nieuwe uitpakken.
// Door pas te verwijderen nadat de zip geldig is gebleken, kan een mislukte of
// verkeerde download nooit een half gesloopte installatie achterlaten.
func replaceCore(zipData []byte, wpRoot string) error {
	nieuw, err := leesCoreZip(zipData)
	if err != nil {
		return err
	}

	for _, dir := range coreDirs {
		if err := os.RemoveAll(filepath.Join(wpRoot, dir)); err != nil {
			return fmt.Errorf("oude %s verwijderen: %w", dir, err)
		}
	}
	if err := verwijderCoreRootBestanden(wpRoot); err != nil {
		return err
	}

	for rel, inhoud := range nieuw {
		doel := filepath.Join(wpRoot, rel)
		if err := os.MkdirAll(filepath.Dir(doel), 0o755); err != nil {
			return fmt.Errorf("map maken voor %s: %w", rel, err)
		}
		if err := os.WriteFile(doel, inhoud, 0o644); err != nil {
			return fmt.Errorf("%s schrijven: %w", rel, err)
		}
	}
	return nil
}

// leesCoreZip valideert de core-zip en geeft de inhoud terug per pad relatief
// aan de webroot (de wordpress/-prefix is gestript). Een zip zonder
// core-mappen of met een pad buiten de doelmap is een fout: dat duidt op een
// verkeerde download (bijv. een HTML-foutpagina) of op zip slip.
func leesCoreZip(zipData []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("core-zip lezen: %w", err)
	}

	bestanden := make(map[string][]byte, len(zr.File))
	heeftCore := false
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rel := strings.TrimPrefix(path.Clean(f.Name), "wordpress/")
		if rel == "" || rel == "." {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("core-zip bevat pad buiten de doelmap: %q", f.Name)
		}
		for _, dir := range coreDirs {
			if strings.HasPrefix(rel, dir+"/") {
				heeftCore = true
				break
			}
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("%s in zip openen: %w", f.Name, err)
		}
		inhoud, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("%s uit zip lezen: %w", f.Name, err)
		}
		bestanden[rel] = inhoud
	}

	if !heeftCore {
		return nil, fmt.Errorf("core-zip bevat geen %s — verkeerde download?", strings.Join(coreDirs, "/"))
	}
	return bestanden, nil
}

// verwijderCoreRootBestanden ruimt de core-bestanden in de webroot op, zodat
// bestanden die WordPress in de nieuwe versie heeft verwijderd ook echt uit de
// repository verdwijnen.
func verwijderCoreRootBestanden(wpRoot string) error {
	items, err := os.ReadDir(wpRoot)
	if err != nil {
		return fmt.Errorf("webroot %s lezen: %w", wpRoot, err)
	}
	for _, item := range items {
		if item.IsDir() || !isCoreRootFile(item.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(wpRoot, item.Name())); err != nil {
			return fmt.Errorf("%s verwijderen: %w", item.Name(), err)
		}
	}
	return nil
}
