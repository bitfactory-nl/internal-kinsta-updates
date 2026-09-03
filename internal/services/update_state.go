package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

// updateState is de runtime-state van de zelf-update: wat er is gecontroleerd,
// wat de gebruiker heeft weggeklikt, en welke changelog bij de laatst
// geïnstalleerde versie hoort. Dit is bewust geen gebruikersinstelling en staat
// daarom niet in config.yml.
type updateState struct {
	LastCheck        time.Time            `json:"last_check"`
	SkippedVersion   string               `json:"skipped_version"`
	LastRunVersion   string               `json:"last_run_version"`
	InstalledVersion string               `json:"installed_version"`
	InstalledChanges []domain.ChangeEntry `json:"installed_changes"`
	InstallLog       string               `json:"install_log"`
}

// DefaultUpdateStatePath is ~/.config/rdm/update-state.json.
func DefaultUpdateStatePath() string {
	home, err := os.UserHomeDir()
	return defaultUpdateStatePathFrom(home, err)
}

// defaultUpdateStatePathFrom bouwt het pad uit een (home, err) paar zoals
// os.UserHomeDir() dat teruggeeft. Nooit terugvallen op een relatief pad: de
// cwd van een .app-bundle is onvoorspelbaar.
func defaultUpdateStatePathFrom(home string, err error) string {
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "rdm", "update-state.json")
	}
	return filepath.Join(home, ".config", "rdm", "update-state.json")
}

// loadUpdateState leest de state. Een ontbrekend bestand is geen fout: dan is
// er nog nooit gecontroleerd.
func loadUpdateState(path string) (updateState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return updateState{}, nil
	}
	if err != nil {
		return updateState{}, fmt.Errorf("update-state lezen: %w", err)
	}

	var st updateState
	if err := json.Unmarshal(data, &st); err != nil {
		return updateState{}, fmt.Errorf("update-state parsen: %w", err)
	}
	return st, nil
}

// saveUpdateState schrijft eerst naar een tijdelijk bestand in dezelfde map en
// hernoemt dat vervolgens. os.Rename is atomair binnen hetzelfde filesystem,
// dus een crash halverwege laat het bestaande bestand ongemoeid in plaats van
// afgekapte JSON achter te laten.
func saveUpdateState(path string, st updateState) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("update-state map aanmaken: %w", err)
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("update-state serialiseren: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".update-state-*.tmp")
	if err != nil {
		return fmt.Errorf("tijdelijk update-state bestand aanmaken: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op als het renamen lukte

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("update-state schrijven: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("update-state sluiten: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("rechten op update-state zetten: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("update-state plaatsen: %w", err)
	}
	return nil
}
