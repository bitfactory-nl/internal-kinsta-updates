package config

import (
	"fmt"
	"os/exec"
	"strings"
)

// keychainService is de service-naam waaronder de tool zijn secrets bewaart.
const keychainService = "nl.nobears.kinsta-updater"

// legacyKeychainService is de service-naam die de tool tot en met v0.2.9
// gebruikte, gebaseerd op een privédomein. MigrateKeychainService haalt
// bestaande keys daar eenmalig weg zodat niemand zijn API-keys opnieuw hoeft in
// te vullen.
const legacyKeychainService = "nl.micromanage.rdm-sites-tool"

// migratedAccounts zijn de vaste keychain-accounts die MigrateKeychainService
// eager overzet bij het opstarten. Dynamisch aangemaakte accounts (zoals
// "ssh:<projectnaam>" van de mediascan) staan hier bewust niet in — die worden
// lui overgezet door keychainGet, de eerste keer dat ze worden opgevraagd.
var migratedAccounts = []string{
	"rdm.kinsta.apiKey",
	"rdm.github.token",
	"rdm.anthropic.apiKey",
	"rdm.wordfence.apiKey",
}

// Hooks op de security-CLI, zodat de migratie te testen is zonder de echte
// keychain aan te raken.
var (
	getFromService = keychainGetFrom
	setInService   = keychainSetIn
)

func keychainGetFrom(service, account string) (string, error) {
	out, err := exec.Command(
		"security", "find-generic-password",
		"-s", service,
		"-a", account,
		"-w",
	).Output()
	if err != nil {
		return "", fmt.Errorf("keychain get %q: %w", account, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func keychainSetIn(service, account, password string) error {
	// -U werkt bij als het item al bestaat, en voegt het anders toe.
	if err := exec.Command(
		"security", "add-generic-password",
		"-s", service,
		"-a", account,
		"-w", password,
		"-U",
	).Run(); err != nil {
		return fmt.Errorf("keychain set %q: %w", account, err)
	}
	return nil
}

// keychainGet leest een secret onder de huidige service-naam. Ontbreekt het
// daar maar staat het nog onder de oude naam — bijvoorbeeld een per project
// aangemaakt "ssh:<naam>"-account dat niet in migratedAccounts staat — dan wordt
// het lui overgezet en alsnog teruggegeven. Zo raakt geen enkel account
// verloren door de naamswijziging, ook niet accounts die later zijn bedacht.
func keychainGet(account string) (string, error) {
	v, err := getFromService(keychainService, account)
	if err == nil && v != "" {
		return v, nil
	}
	legacy, legacyErr := getFromService(legacyKeychainService, account)
	if legacyErr != nil || legacy == "" {
		// De oorspronkelijke fout teruggeven: die beschrijft het account dat de
		// aanroeper vroeg, niet de fallback.
		if err == nil {
			err = fmt.Errorf("keychain get %q: leeg", account)
		}
		return "", err
	}
	// Best effort: mislukt het kopiëren, dan werkt de waarde nog steeds en
	// probeert de volgende aanroep het opnieuw.
	_ = setInService(keychainService, account, legacy)
	return legacy, nil
}

func KeychainSet(account, password string) error {
	return setInService(keychainService, account, password)
}

func KeychainDelete(account string) error {
	if err := exec.Command(
		"security", "delete-generic-password",
		"-s", keychainService,
		"-a", account,
	).Run(); err != nil {
		return fmt.Errorf("keychain delete %q: %w", account, err)
	}
	return nil
}

// MigrateKeychainService zet secrets die nog onder de oude service-naam staan
// over naar de nieuwe, en geeft terug welke accounts zijn overgezet. Best
// effort en idempotent: een account dat onder de nieuwe naam al een waarde
// heeft blijft ongemoeid, en fouten worden stil overgeslagen — een mislukte
// migratie mag het opstarten nooit blokkeren.
//
// De oude items worden bewust niet verwijderd: een delete op de keychain kan om
// toestemming vragen, en zo'n prompt tijdens het opstarten is een slechtere
// ervaring dan een achtergebleven item dat de gebruiker desgewenst zelf in
// Keychain Access opruimt.
func MigrateKeychainService() []string {
	var migrated []string
	for _, account := range migratedAccounts {
		if v, err := getFromService(keychainService, account); err == nil && v != "" {
			continue
		}
		v, err := getFromService(legacyKeychainService, account)
		if err != nil || v == "" {
			continue
		}
		if err := setInService(keychainService, account, v); err != nil {
			continue
		}
		migrated = append(migrated, account)
	}
	return migrated
}
