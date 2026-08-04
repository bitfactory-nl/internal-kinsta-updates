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

// envArgs zet "KEY=VAL"-paren om in docker-exec's -e-vlaggen.
func envArgs(env []string) []string {
	args := make([]string, 0, len(env)*2)
	for _, e := range env {
		args = append(args, "-e", e)
	}
	return args
}

// Exec draait een commando in een lopende container via `docker exec -i`,
// met stdin/stdout doorgesluisd en optionele omgevingsvariabelen (bv.
// MYSQL_PWD, zodat een wachtwoord niet als los argument op de commandline
// hoeft te staan).
func Exec(ctx context.Context, container string, args []string, env []string, stdin io.Reader, stdout io.Writer) error {
	full := append([]string{"exec", "-i"}, envArgs(env)...)
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
