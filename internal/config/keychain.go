package config

import (
	"fmt"
	"os/exec"
	"strings"
)

const keychainService = "nl.micromanage.rdm-sites-tool"

func keychainGet(account string) (string, error) {
	out, err := exec.Command(
		"security", "find-generic-password",
		"-s", keychainService,
		"-a", account,
		"-w",
	).Output()
	if err != nil {
		return "", fmt.Errorf("keychain get %q: %w", account, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func KeychainSet(account, password string) error {
	// Try to update first, then add
	err := exec.Command(
		"security", "add-generic-password",
		"-s", keychainService,
		"-a", account,
		"-w", password,
		"-U",
	).Run()
	if err != nil {
		return fmt.Errorf("keychain set %q: %w", account, err)
	}
	return nil
}

func KeychainDelete(account string) error {
	err := exec.Command(
		"security", "delete-generic-password",
		"-s", keychainService,
		"-a", account,
	).Run()
	if err != nil {
		return fmt.Errorf("keychain delete %q: %w", account, err)
	}
	return nil
}
