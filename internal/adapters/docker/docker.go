// Package docker runs commands inside the local docker-compose containers
// that host the developer's WordPress databases (bitf-mysql, bitf-mysql84).
package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Test seams: zo kan een test doen alsof docker ergens anders staat, of het
// uitvoeren van docker exec zelf vervangen.
var (
	dockerLookPath   = exec.LookPath
	dockerVastePaden = []string{
		"/usr/local/bin/docker",                                  // Docker Desktop op Intel, of handmatig geinstalleerd
		"/opt/homebrew/bin/docker",                               // Docker Desktop op Apple Silicon (Homebrew-symlink)
		"/Applications/Docker.app/Contents/Resources/bin/docker", // Docker Desktop, rechtstreeks
	}
	dockerEenmalig sync.Once
	dockerGevonden string

	execCommandContext = exec.CommandContext
)

// DockerBin zoekt de docker-binary en onthoudt de uitkomst.
//
// Op PATH vertrouwen kan niet: een .app die vanuit Finder of /Applications wordt
// geopend erft de shell-PATH niet, maar krijgt alleen /usr/bin:/bin:/usr/sbin:/sbin.
// Docker Desktop staat daar niet in, en dan faalt elke docker-aanroep met
// "executable file not found" — terwijl dezelfde machine in een terminal prima
// werkt. Zelfde bug, zelfde oplossing als NodeBin() in internal/adapters/browser.
func DockerBin() string {
	dockerEenmalig.Do(func() { dockerGevonden = zoekDocker() })
	return dockerGevonden
}

func zoekDocker() string {
	if p := strings.TrimSpace(os.Getenv("RDM_DOCKER")); p != "" {
		return p
	}
	if p, err := dockerLookPath("docker"); err == nil && p != "" {
		return p
	}
	for _, kandidaat := range dockerVastePaden {
		if uitvoerbaar(kandidaat) {
			return kandidaat
		}
	}
	if p := viaLoginShell(); p != "" {
		return p
	}
	// Niets gevonden: "docker" teruggeven levert een foutmelding op die tenminste
	// het woord "docker" noemt, in plaats van een leeg pad dat niets zegt.
	return "docker"
}

func uitvoerbaar(pad string) bool {
	st, err := os.Stat(pad)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// viaLoginShell vraagt de loginshell waar docker staat. Dat kent elke exotische
// installatie, maar het start wel de rc-bestanden van de gebruiker op — vandaar
// een korte tijdslimiet en pas als laatste redmiddel.
func viaLoginShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, shell, "-ilc", "command -v docker").Output()
	if err != nil {
		return ""
	}
	pad := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if uitvoerbaar(pad) {
		return pad
	}
	return ""
}

// schrijfEnvBestand zet "KEY=VAL"-paren om in een tijdelijk --env-file voor
// docker exec. Dat is bewust geen -e KEY=VAL op de commandline: die argumenten
// staan zolang het proces loopt zichtbaar in `ps aux` op deze machine (ook al
// is het wachtwoord hier een well-known lokale dev-default, geen echt geheim).
// Het bestand krijgt 0600 en wordt na afloop verwijderd.
func schrijfEnvBestand(env []string) (string, error) {
	if len(env) == 0 {
		return "", nil
	}
	f, err := os.CreateTemp("", "rdm-docker-env-*")
	if err != nil {
		return "", fmt.Errorf("tijdelijk env-bestand aanmaken: %w", err)
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	if _, err := f.WriteString(strings.Join(env, "\n") + "\n"); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// Exec draait een commando in een lopende container via `docker exec -i`,
// met stdin/stdout doorgesluisd en optionele omgevingsvariabelen (bv.
// MYSQL_PWD) via een tijdelijk --env-file, zodat een wachtwoord niet als los
// argument op de commandline hoeft te staan.
func Exec(ctx context.Context, container string, args []string, env []string, stdin io.Reader, stdout io.Writer) error {
	envFile, err := schrijfEnvBestand(env)
	if err != nil {
		return err
	}
	if envFile != "" {
		defer os.Remove(envFile)
	}

	full := []string{"exec", "-i"}
	if envFile != "" {
		full = append(full, "--env-file", envFile)
	}
	full = append(full, container)
	full = append(full, args...)

	cmd := execCommandContext(ctx, DockerBin(), full...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker exec op %q mislukt: %w: %s", container, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
