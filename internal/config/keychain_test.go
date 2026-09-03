package config

import (
	"errors"
	"fmt"
	"testing"
)

// nepKeychain vervangt de macOS security-CLI: een map per service-naam, zodat
// de migratie te testen is zonder de echte keychain van de ontwikkelaar aan te
// raken (die immers prompts kan opleveren en per machine verschilt).
type nepKeychain struct {
	items map[string]map[string]string // service -> account -> waarde
	sets  int
}

func newNepKeychain() *nepKeychain {
	return &nepKeychain{items: map[string]map[string]string{}}
}

func (n *nepKeychain) get(service, account string) (string, error) {
	v, ok := n.items[service][account]
	if !ok {
		return "", errors.New("niet gevonden")
	}
	return v, nil
}

func (n *nepKeychain) set(service, account, waarde string) error {
	if n.items[service] == nil {
		n.items[service] = map[string]string{}
	}
	n.items[service][account] = waarde
	n.sets++
	return nil
}

// installeerNep hangt de nepkeychain aan de hooks en zet ze na de test terug.
func installeerNep(t *testing.T, n *nepKeychain) {
	t.Helper()
	origGet, origSet := getFromService, setInService
	getFromService, setInService = n.get, n.set
	t.Cleanup(func() { getFromService, setInService = origGet, origSet })
}

func TestMigrateKeychainServiceZetOudeKeysOver(t *testing.T) {
	n := newNepKeychain()
	_ = n.set(legacyKeychainService, "rdm.kinsta.apiKey", "kinsta-geheim")
	_ = n.set(legacyKeychainService, "rdm.github.token", "ghp_oud")
	n.sets = 0
	installeerNep(t, n)

	migrated := MigrateKeychainService()

	if len(migrated) != 2 {
		t.Fatalf("migrated = %v, wil 2 accounts", migrated)
	}
	if got := n.items[keychainService]["rdm.kinsta.apiKey"]; got != "kinsta-geheim" {
		t.Errorf("kinsta-key onder nieuwe service = %q, wil %q", got, "kinsta-geheim")
	}
	if got := n.items[keychainService]["rdm.github.token"]; got != "ghp_oud" {
		t.Errorf("github-token onder nieuwe service = %q, wil %q", got, "ghp_oud")
	}
	// De oude items blijven staan: verwijderen kan een keychain-prompt geven.
	if got := n.items[legacyKeychainService]["rdm.kinsta.apiKey"]; got != "kinsta-geheim" {
		t.Errorf("oude kinsta-key = %q, wil ongemoeid", got)
	}
}

func TestMigrateKeychainServiceLaatNieuweWaardeStaan(t *testing.T) {
	n := newNepKeychain()
	_ = n.set(legacyKeychainService, "rdm.kinsta.apiKey", "oud")
	_ = n.set(keychainService, "rdm.kinsta.apiKey", "nieuw")
	installeerNep(t, n)

	if migrated := MigrateKeychainService(); len(migrated) != 0 {
		t.Fatalf("migrated = %v, wil leeg", migrated)
	}
	if got := n.items[keychainService]["rdm.kinsta.apiKey"]; got != "nieuw" {
		t.Errorf("waarde = %q, wil de bestaande %q", got, "nieuw")
	}
}

func TestMigrateKeychainServiceZonderOudeKeys(t *testing.T) {
	n := newNepKeychain()
	installeerNep(t, n)

	if migrated := MigrateKeychainService(); len(migrated) != 0 {
		t.Fatalf("migrated = %v, wil leeg", migrated)
	}
	if n.sets != 0 {
		t.Errorf("aantal writes = %d, wil 0", n.sets)
	}
}

func TestMigrateKeychainServiceIsIdempotent(t *testing.T) {
	n := newNepKeychain()
	_ = n.set(legacyKeychainService, "rdm.wordfence.apiKey", "wf")
	installeerNep(t, n)

	eerste := MigrateKeychainService()
	tweede := MigrateKeychainService()

	if len(eerste) != 1 {
		t.Fatalf("eerste ronde = %v, wil 1 account", eerste)
	}
	if len(tweede) != 0 {
		t.Fatalf("tweede ronde = %v, wil leeg", tweede)
	}
}

func TestMigratedAccountsDekkenAlleServiceNamen(t *testing.T) {
	// Vangt de fout waarbij later een nieuwe keychain-key wordt toegevoegd
	// zonder die in de migratielijst op te nemen.
	wil := []string{
		"rdm.kinsta.apiKey",
		"rdm.github.token",
		"rdm.anthropic.apiKey",
		"rdm.wordfence.apiKey",
	}
	if len(migratedAccounts) != len(wil) {
		t.Fatalf("migratedAccounts = %v, wil %v", migratedAccounts, wil)
	}
	for i, acct := range wil {
		if migratedAccounts[i] != acct {
			t.Errorf("migratedAccounts[%d] = %q, wil %q", i, migratedAccounts[i], acct)
		}
	}
}

func TestKeychainServiceNamen(t *testing.T) {
	if keychainService != "nl.nobears.kinsta-updater" {
		t.Errorf("keychainService = %q, wil nl.nobears.kinsta-updater", keychainService)
	}
	if legacyKeychainService != "nl.micromanage.rdm-sites-tool" {
		t.Errorf("legacyKeychainService = %q, wil de oude naam", legacyKeychainService)
	}
	_ = fmt.Sprint(keychainService) // houdt de import in gebruik bij aanpassingen
}
