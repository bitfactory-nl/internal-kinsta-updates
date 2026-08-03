package browser

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Test seams: zo kan een test doen alsof node ergens anders staat.
var (
	nodeLookPath   = exec.LookPath
	nodeVastePaden = []string{
		"/opt/homebrew/bin/node", // Homebrew op Apple Silicon
		"/usr/local/bin/node",    // Homebrew op Intel, of handmatig geïnstalleerd
		"/opt/local/bin/node",    // MacPorts
		"/usr/bin/node",
	}
	nodeEenmalig sync.Once
	nodeGevonden string
)

// NodeBin zoekt de node-binary en onthoudt de uitkomst.
//
// Op PATH vertrouwen kan niet: een .app die vanuit Finder of /Applications wordt
// geopend erft de shell-PATH niet, maar krijgt alleen /usr/bin:/bin:/usr/sbin:/sbin.
// Homebrew en nvm staan daar niet in, en dan faalt elke sidecar met
// "node: executable file not found" — terwijl dezelfde machine in een terminal prima
// werkt. Daarom: PATH proberen, dan de vaste plekken, dan de versiebeheerders, en als
// laatste de loginshell vragen (die kent élke installatie, maar is traag).
func NodeBin() string {
	nodeEenmalig.Do(func() { nodeGevonden = zoekNode() })
	return nodeGevonden
}

func zoekNode() string {
	if p := strings.TrimSpace(os.Getenv("RDM_NODE")); p != "" {
		return p
	}
	if p, err := nodeLookPath("node"); err == nil && p != "" {
		return p
	}
	for _, kandidaat := range nodeVastePaden {
		if uitvoerbaar(kandidaat) {
			return kandidaat
		}
	}
	if p := viaVersiebeheerder(); p != "" {
		return p
	}
	if p := viaLoginShell(); p != "" {
		return p
	}
	// Niets gevonden: "node" teruggeven levert een foutmelding op die verduidelijk()
	// omzet in een instructie, in plaats van een leeg pad dat niets zegt.
	return "node"
}

func uitvoerbaar(pad string) bool {
	st, err := os.Stat(pad)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// viaVersiebeheerder zoekt de installaties van nvm, fnm, volta en asdf. Bij meerdere
// versies wint de hoogste, want een oude Node haalt de eisen van de sidecar niet.
func viaVersiebeheerder() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, vast := range []string{
		filepath.Join(home, ".volta", "bin", "node"),
		filepath.Join(home, ".asdf", "shims", "node"),
	} {
		if uitvoerbaar(vast) {
			return vast
		}
	}

	for _, patroon := range []string{
		filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "node"),
		filepath.Join(home, ".local", "share", "fnm", "node-versions", "*", "installation", "bin", "node"),
		filepath.Join(home, "Library", "Application Support", "fnm", "node-versions", "*", "installation", "bin", "node"),
	} {
		treffers, _ := filepath.Glob(patroon)
		treffers = filterUitvoerbaar(treffers)
		if len(treffers) == 0 {
			continue
		}
		sort.Slice(treffers, func(i, j int) bool {
			return versieLager(versieUitPad(treffers[i]), versieUitPad(treffers[j]))
		})
		return treffers[len(treffers)-1]
	}
	return ""
}

func filterUitvoerbaar(paden []string) []string {
	uit := paden[:0]
	for _, p := range paden {
		if uitvoerbaar(p) {
			uit = append(uit, p)
		}
	}
	return uit
}

// versieUitPad haalt "20.11.0" uit .../node/v20.11.0/bin/node.
func versieUitPad(pad string) string {
	delen := strings.Split(filepath.ToSlash(pad), "/")
	for _, d := range delen {
		if len(d) > 1 && (d[0] == 'v' || (d[0] >= '0' && d[0] <= '9')) && strings.Contains(d, ".") {
			return strings.TrimPrefix(d, "v")
		}
	}
	return ""
}

// versieLager vergelijkt op getal per onderdeel, zodat 9.0.0 niet boven 22.3.0 komt
// zoals bij tekstvergelijking zou gebeuren.
func versieLager(a, b string) bool {
	da, db := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(da) || i < len(db); i++ {
		var na, nb int
		if i < len(da) {
			na, _ = strconv.Atoi(da[i])
		}
		if i < len(db) {
			nb, _ = strconv.Atoi(db[i])
		}
		if na != nb {
			return na < nb
		}
	}
	return false
}

// viaLoginShell vraagt de loginshell waar node staat. Dat kent elke exotische
// installatie, maar het start wel de rc-bestanden van de gebruiker op — vandaar een
// korte tijdslimiet en pas als laatste redmiddel.
func viaLoginShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, shell, "-ilc", "command -v node").Output()
	if err != nil {
		return ""
	}
	pad := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if uitvoerbaar(pad) {
		return pad
	}
	return ""
}
