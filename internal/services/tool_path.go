package services

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Een .app die vanuit Finder of /Applications start, erft de shell-PATH niet: hij
// krijgt alleen PATH=/usr/bin:/bin:/usr/sbin:/sbin. Dat is precies waarom een
// Makefile-target in de dev-build werkt (gestart uit een terminal, met volledige
// PATH) en in de geïnstalleerde app faalt met
//
//	make: docker-compose: No such file or directory
//
// `make` zelf staat in /usr/bin en wordt dus gevonden; wat het recept aanroept —
// docker-compose, docker, npm — staat in Homebrew of in Docker.app en valt
// buiten die minimale PATH. Vandaar dat elk kindproces dat projectcommando's
// draait een verrijkte PATH mee moet krijgen.
//
// Dit is dezelfde afweging als in NodeBin/DockerBin, maar omgekeerd: daar wordt
// één binary gezocht, hier moet een heel recept zijn eigen gereedschap kunnen
// vinden.

// gereedschapDirs zijn de plekken waar ontwikkelgereedschap op macOS staat, in
// de volgorde waarin ze voorrang krijgen.
var gereedschapDirs = []string{
	"/opt/homebrew/bin",  // Homebrew op Apple Silicon
	"/opt/homebrew/sbin", //
	"/usr/local/bin",     // Homebrew op Intel, Docker Desktop
	"/usr/local/sbin",    //
	"/Applications/Docker.app/Contents/Resources/bin", // docker/docker-compose uit Docker Desktop
	"/opt/local/bin", // MacPorts
}

// Test seams.
var (
	padBestaat           = standaardPadBestaat
	loginShellPATHFn     = loginShellPATH
	gereedschapEenmalig  sync.Once
	gereedschapPATHCache string
)

func standaardPadBestaat(pad string) bool {
	st, err := os.Stat(pad)
	return err == nil && st.IsDir()
}

// GereedschapPATH geeft een PATH waarin projectgereedschap te vinden is. De
// bestaande PATH blijft erin staan: er wordt alleen aangevuld, nooit vervangen,
// zodat een gebruiker met eigen instellingen niets kwijtraakt.
func GereedschapPATH() string {
	gereedschapEenmalig.Do(func() { gereedschapPATHCache = bouwGereedschapPATH() })
	return gereedschapPATHCache
}

func bouwGereedschapPATH() string {
	huidig := os.Getenv("PATH")
	var delen []string

	// De gebruikersmap kan nog Docker-CLI-plugins hebben.
	if home, err := os.UserHomeDir(); err == nil {
		if p := filepath.Join(home, ".docker", "bin"); padBestaat(p) {
			delen = append(delen, p)
		}
	}
	for _, dir := range gereedschapDirs {
		if padBestaat(dir) {
			delen = append(delen, dir)
		}
	}

	// Alleen als de PATH er echt uitgeklede uitziet, de loginshell erbij halen.
	// Die kent ook asdf, nvm en andere eigen installaties, maar hij start de
	// rc-bestanden van de gebruiker op en is dus traag — vandaar niet standaard.
	if isMinimalePATH(huidig) {
		if extra := loginShellPATHFn(); extra != "" {
			delen = append(delen, strings.Split(extra, ":")...)
		}
	}

	delen = append(delen, strings.Split(huidig, ":")...)
	return strings.Join(uniekeNietLegeDelen(delen), ":")
}

// isMinimalePATH herkent de PATH die macOS aan een uit Finder gestarte .app
// geeft: geen enkele plek waar Homebrew of Docker Desktop staat.
func isMinimalePATH(pad string) bool {
	for _, deel := range strings.Split(pad, ":") {
		switch deel {
		case "/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin":
			return false
		}
	}
	return true
}

func loginShellPATH() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, shell, "-ilc", "printf %s \"$PATH\"").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func uniekeNietLegeDelen(delen []string) []string {
	gezien := map[string]bool{}
	uit := make([]string, 0, len(delen))
	for _, d := range delen {
		d = strings.TrimSpace(d)
		if d == "" || gezien[d] {
			continue
		}
		gezien[d] = true
		uit = append(uit, d)
	}
	return uit
}

// MetPATH vervangt de PATH-regel in env, of voegt hem toe als die er niet staat.
// Bewust vervangen en niet dubbel toevoegen: dat maakt zichtbaar wat het
// kindproces krijgt in plaats van te leunen op de manier waarop os/exec
// duplicaten oplost.
func MetPATH(env []string, pad string) []string {
	uit := make([]string, 0, len(env)+1)
	gezet := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			if gezet {
				continue
			}
			uit = append(uit, "PATH="+pad)
			gezet = true
			continue
		}
		uit = append(uit, kv)
	}
	if !gezet {
		uit = append(uit, "PATH="+pad)
	}
	return uit
}
