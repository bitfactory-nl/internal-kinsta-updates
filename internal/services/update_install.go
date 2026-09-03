package services

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

//go:embed update_script.sh.tmpl
var updateScriptTemplate embed.FS

// stagedBinaryName is de naam van het uitvoerbare bestand in de bundle; komt
// uit CFBundleExecutable in build/darwin/Info.plist.
const stagedBinaryName = "rdm-sites-tool"

// scriptData zijn de waarden die in het helper-script worden ingevuld.
type scriptData struct {
	PID        int
	BundlePath string
	StagedApp  string
	LogPath    string
}

// Install haalt de beschikbare update binnen, zet hem klaar in een tempmap, en
// draagt het vervangen over aan een los script. Bij succes keert deze functie
// niet terug op een zinvolle manier: de app sluit zichzelf af.
//
// De volgorde is bewust: alles wat kan mislukken gebeurt vóórdat er iets aan de
// bestaande installatie verandert. Faalt de download, het uitpakken of de
// controle, dan staat er nog steeds een werkende app.
func (s *UpdateService) Install() error {
	if !s.enabled() {
		return fmt.Errorf("zelf-update is uitgeschakeld in deze build (versie %q)", s.current)
	}

	s.mu.Lock()
	beschikbaar := s.available
	asset := s.asset
	s.mu.Unlock()

	if beschikbaar == nil || asset.ID == 0 {
		return fmt.Errorf("geen update beschikbaar om te installeren; controleer eerst op updates")
	}

	// Kan de bestaande installatie überhaupt vervangen worden? Dit eerst
	// vragen, zodat een gebruiker zonder schrijfrechten niet eerst 12 MB
	// downloadt om daarna alsnog te stranden.
	doelMap := filepath.Dir(s.bundlePath)
	if err := mapIsBeschrijfbaar(doelMap); err != nil {
		return fmt.Errorf("geen schrijfrechten op %s: installeer de update handmatig (%w)", doelMap, err)
	}

	token, err := s.token()
	if err != nil {
		return err
	}

	werkMap, err := os.MkdirTemp("", "rdm-update-*")
	if err != nil {
		return fmt.Errorf("tijdelijke map aanmaken: %w", err)
	}
	// De werkmap blijft staan tot het script klaar is; die ruimt macOS zelf op.

	zipPad := filepath.Join(werkMap, "update.zip")
	if err := s.downloadNaar(token, asset.ID, zipPad); err != nil {
		return err
	}

	uitgepakt := filepath.Join(werkMap, "uitgepakt")
	s.emitProgress(domain.UpdateProgress{Phase: domain.PhaseUitpakken})
	if err := os.MkdirAll(uitgepakt, 0o755); err != nil {
		return fmt.Errorf("uitpakmap aanmaken: %w", err)
	}
	// ditto pakt uit met behoud van symlinks en rechten; unzip doet dat niet
	// betrouwbaar voor een .app-bundle.
	if out, err := exec.Command("ditto", "-x", "-k", zipPad, uitgepakt).CombinedOutput(); err != nil {
		return fmt.Errorf("update uitpakken: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	nieuweApp, err := validateStagedApp(uitgepakt)
	if err != nil {
		return err
	}

	logPad := filepath.Join(s.logDir, fmt.Sprintf("update-%s.log", time.Now().Format("20060102-150405")))

	// De changelog nu bewaren: na de herstart is er geen netwerk of token nodig
	// om te tonen wat er veranderd is.
	st, _ := loadUpdateState(s.statePath)
	st.InstalledVersion = beschikbaar.Version
	st.InstalledChanges = beschikbaar.Changes
	st.InstallLog = logPad
	if err := saveUpdateState(s.statePath, st); err != nil {
		return err
	}

	scriptPad := filepath.Join(werkMap, "update.sh")
	if err := renderUpdateScript(scriptPad, scriptData{
		PID:        os.Getpid(),
		BundlePath: s.bundlePath,
		StagedApp:  nieuweApp,
		LogPath:    logPad,
	}); err != nil {
		return err
	}

	s.emitProgress(domain.UpdateProgress{Phase: domain.PhaseVervangen})
	if err := startLosgekoppeld(scriptPad); err != nil {
		return err
	}

	// Even ademruimte zodat de frontend de laatste voortgangsstap nog toont.
	time.Sleep(300 * time.Millisecond)
	if s.app != nil {
		s.app.Quit()
	}
	return nil
}

// downloadNaar streamt de asset naar pad en meldt de voortgang.
func (s *UpdateService) downloadNaar(token string, assetID int64, pad string) error {
	f, err := os.Create(pad)
	if err != nil {
		return fmt.Errorf("downloadbestand aanmaken: %w", err)
	}
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	err = s.newClient(token, selfRepo).DownloadAsset(ctx, assetID, f, func(done, total int64) {
		s.emitProgress(domain.UpdateProgress{Phase: domain.PhaseDownload, Done: done, Total: total})
	})
	if err != nil {
		return fmt.Errorf("update downloaden: %w", err)
	}
	return f.Sync()
}

// validateStagedApp controleert dat het uitgepakte archief precies één
// app-bundle bevat die er compleet uitziet, en geeft het pad daarvan terug.
func validateStagedApp(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("uitgepakte update lezen: %w", err)
	}

	var bundles []string
	for _, e := range entries {
		if e.IsDir() && filepath.Ext(e.Name()) == ".app" {
			bundles = append(bundles, filepath.Join(root, e.Name()))
		}
	}

	switch {
	case len(bundles) == 0:
		return "", fmt.Errorf("de gedownloade update bevat geen .app-bundle")
	case len(bundles) > 1:
		return "", fmt.Errorf("de gedownloade update bevat meer dan één .app-bundle (%d); dat hoort niet en wordt niet geïnstalleerd", len(bundles))
	}

	app := bundles[0]
	if _, err := os.Stat(filepath.Join(app, "Contents", "Info.plist")); err != nil {
		return "", fmt.Errorf("de gedownloade update mist Contents/Info.plist")
	}

	bin := filepath.Join(app, "Contents", "MacOS", stagedBinaryName)
	info, err := os.Stat(bin)
	if err != nil {
		return "", fmt.Errorf("de gedownloade update mist Contents/MacOS/%s", stagedBinaryName)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("het binaire bestand in de gedownloade update is niet uitvoerbaar")
	}
	return app, nil
}

// renderUpdateScript schrijft het helper-script naar pad en maakt het
// uitvoerbaar.
func renderUpdateScript(pad string, d scriptData) error {
	tmpl, err := template.ParseFS(updateScriptTemplate, "update_script.sh.tmpl")
	if err != nil {
		return fmt.Errorf("update-script template lezen: %w", err)
	}

	f, err := os.OpenFile(pad, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return fmt.Errorf("update-script aanmaken: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, d); err != nil {
		return fmt.Errorf("update-script schrijven: %w", err)
	}
	return nil
}

// startLosgekoppeld start het script in een eigen sessie, zodat het blijft
// draaien nadat deze app is afgesloten.
func startLosgekoppeld(scriptPad string) error {
	cmd := exec.Command("/bin/bash", scriptPad)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("update-script starten: %w", err)
	}
	// Niet wachten: het script overleeft dit proces met opzet.
	go func() { _ = cmd.Wait() }()
	return nil
}

// mapIsBeschrijfbaar meldt of de map beschrijfbaar is voor de huidige
// gebruiker, getest door er een bestand in aan te maken en weer te verwijderen.
func mapIsBeschrijfbaar(dir string) error {
	f, err := os.CreateTemp(dir, ".rdm-schrijftest-*")
	if err != nil {
		return err
	}
	naam := f.Name()
	f.Close()
	return os.Remove(naam)
}
