package config

import (
	"errors"
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

// securityStdin draait de security-CLI in interactieve modus en geeft het
// commando via stdin door. Zo staat het secret nooit in de procesargumenten,
// die andere lokale processen via ps kunnen lezen.
var securityStdin = func(input string) error {
	cmd := exec.Command("security", "-i")
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	return securityResult(out, err)
}

// securityResult zet de uitvoer van security -i om in een fout. Een mislukt
// commando geeft exit ≠ 0 met de reden op de eerste regel; bij exit 0 is er
// normaal geen uitvoer, dus uitvoer geldt dan alsnog als fout. Alleen de
// eerste regel komt in de melding, de rest kan invoer herhalen.
func securityResult(out []byte, err error) error {
	regel := eersteRegel(string(out))
	switch {
	case err != nil && regel != "":
		return fmt.Errorf("%w: %s", err, regel)
	case err != nil:
		return err
	case regel != "":
		return errors.New(regel)
	}
	return nil
}

// eersteRegel geeft de eerste niet-lege regel, zonder witruimte eromheen.
func eersteRegel(s string) string {
	for _, r := range strings.Split(s, "\n") {
		if r = strings.TrimSpace(r); r != "" {
			return r
		}
	}
	return ""
}

// quoteSecurity zet een waarde tussen dubbele quotes zoals security -i die
// parset: backslash en dubbele quote krijgen een backslash ervoor.
func quoteSecurity(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// addPasswordCommand bouwt één regel voor security -i. Een regeleinde zou de
// regel afbreken en de rest als nieuw commando laten lezen, dus dat weigeren we.
func addPasswordCommand(service, account, password string) (string, error) {
	for _, v := range []string{service, account, password} {
		if strings.ContainsAny(v, "\r\n") {
			return "", errors.New("keychain: waarde mag geen regeleinde bevatten")
		}
	}
	// -U werkt bij als het item al bestaat, en voegt het anders toe.
	return fmt.Sprintf("add-generic-password -s %s -a %s -w %s -U\n",
		quoteSecurity(service), quoteSecurity(account), quoteSecurity(password)), nil
}

func keychainSetIn(service, account, password string) error {
	line, err := addPasswordCommand(service, account, password)
	if err != nil {
		return err
	}
	if err := securityStdin(line); err != nil {
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
