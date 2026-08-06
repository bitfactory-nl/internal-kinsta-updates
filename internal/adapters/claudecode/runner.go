// Package claudecode drives the Claude Code CLI in headless mode, so the tool
// can let an agent work in a project checkout: read the code, change files and
// run its own checks.
//
// Dit is bewust de CLI en niet de Messages API. De bestaande claude-adapter doet
// losse tool-calls (één vraag, één antwoord); een fout opsporen in een codebase
// vraagt rondkijken, lezen en herhaald aanpassen. Dat is precies wat de CLI al
// kan, en die staat toch al op de machine van de gebruiker.
package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Test seams.
var (
	claudeLookPath     = exec.LookPath
	execCommandContext = exec.CommandContext
	claudeVastePaden   = []string{
		"/opt/homebrew/bin/claude", // Homebrew op Apple Silicon
		"/usr/local/bin/claude",    // Homebrew op Intel
	}
	claudeEenmalig sync.Once
	claudeGevonden string
)

// Bin zoekt de claude-binary en onthoudt de uitkomst.
//
// Net als bij node kan er niet op PATH vertrouwd worden: een .app die vanuit
// Finder start erft de shell-PATH niet. Vandaar dezelfde reeks: omgevingsvariabele,
// PATH, vaste plekken, en als laatste de loginshell.
func Bin() string {
	claudeEenmalig.Do(func() { claudeGevonden = zoekClaude() })
	return claudeGevonden
}

func zoekClaude() string {
	if p := strings.TrimSpace(os.Getenv("RDM_CLAUDE_BIN")); p != "" {
		return p
	}
	if p, err := claudeLookPath("claude"); err == nil && p != "" {
		return p
	}
	for _, kandidaat := range claudeVastePaden {
		if uitvoerbaar(kandidaat) {
			return kandidaat
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, kandidaat := range []string{
			home + "/.claude/local/claude",
			home + "/.local/bin/claude",
		} {
			if uitvoerbaar(kandidaat) {
				return kandidaat
			}
		}
	}
	if p := viaLoginShell(); p != "" {
		return p
	}
	return "claude"
}

func uitvoerbaar(pad string) bool {
	st, err := os.Stat(pad)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

func viaLoginShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, shell, "-ilc", "command -v claude").Output()
	if err != nil {
		return ""
	}
	pad := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if uitvoerbaar(pad) {
		return pad
	}
	return ""
}

// Opties bepaalt hoe de agent draait.
type Opties struct {
	// Dir is de werkmap; de agent mag niets daarbuiten aanraken.
	Dir string
	// Model is een alias (sonnet, opus) of een volledige model-id. Leeg laat de
	// CLI zijn eigen standaard kiezen.
	Model string
	// ToegestaneTools beperkt wat de agent mag. Leeg betekent: de standaardset
	// van de CLI.
	ToegestaneTools []string
	// VerbodenTools sluit tools expliciet uit.
	VerbodenTools []string
	// APIKey wordt als ANTHROPIC_API_KEY meegegeven wanneer de omgeving die nog
	// niet heeft. Dat is de terugval voor het geval de CLI niet is ingelogd:
	// zonder dit faalt een headless run met "OAuth session expired". Een
	// bestaande ANTHROPIC_API_KEY in de omgeving wordt nooit overschreven, want
	// dan bepaalt de gebruiker zelf waar de kosten landen.
	APIKey string
	// PATH is de PATH die de agent meekrijgt. Leeg betekent: die van dit proces.
	// Dit moet gezet worden wanneer de app uit Finder start, want dan is de
	// geërfde PATH alleen /usr/bin:/bin:/usr/sbin:/sbin en vindt de agent geen
	// php, composer of npm om zijn eigen werk te controleren.
	PATH string
	// OnProgress krijgt een korte, mensvriendelijke regel per stap.
	OnProgress func(string)
}

// Resultaat is de uitkomst van één run.
type Resultaat struct {
	// Samenvatting is de laatste tekst die de agent teruggaf.
	Samenvatting string
	// Gereedschappen zijn de tools die de agent gebruikte, in volgorde.
	Gereedschappen []string
	// Turns is het aantal assistant-berichten; handig om een vastloper te zien.
	Turns int
	// Ruw is de volledige stream, voor foutdiagnose.
	Ruw string
}

// Run voert één headless sessie uit en wacht tot die klaar is.
//
// De begrenzing is de context: de CLI in gebruik (2.1.x) heeft geen --max-turns,
// dus een deadline op de context is het enige dat een uitlopende agent stopt.
func Run(ctx context.Context, prompt string, o Opties) (Resultaat, error) {
	if strings.TrimSpace(prompt) == "" {
		return Resultaat{}, fmt.Errorf("lege opdracht")
	}
	if o.Dir == "" {
		return Resultaat{}, fmt.Errorf("werkmap ontbreekt")
	}
	if st, err := os.Stat(o.Dir); err != nil || !st.IsDir() {
		return Resultaat{}, fmt.Errorf("werkmap %q bestaat niet", o.Dir)
	}

	cmd := execCommandContext(ctx, Bin(), Args(prompt, o)...)
	cmd.Dir = o.Dir
	// De agent erft de omgeving, want die heeft zijn eigen configuratie en
	// inloggegevens nodig.
	cmd.Env = Omgeving(os.Environ(), o.APIKey, o.PATH)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Resultaat{}, fmt.Errorf("stdout koppelen: %w", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return Resultaat{}, fmt.Errorf("claude starten: %w (gezocht als %q)", err, Bin())
	}

	res, leesErr := LeesStream(stdout, o.OnProgress)
	waitErr := cmd.Wait()

	if waitErr != nil {
		melding := strings.TrimSpace(stderr.String())
		if melding == "" {
			melding = strings.TrimSpace(res.Samenvatting)
		}
		if ctx.Err() != nil {
			return res, fmt.Errorf("de AI-run is afgebroken na de tijdslimiet: %w", ctx.Err())
		}
		return res, verduidelijk(fmt.Errorf("claude eindigde met een fout: %v: %s", waitErr, kapMelding(melding)))
	}
	if leesErr != nil {
		return res, verduidelijk(leesErr)
	}
	return res, nil
}

// verduidelijk turns the CLI's own diagnostics into something the user can act
// on. Een verlopen sessie is de meest waarschijnlijke storing en de melding van
// de CLI zegt niet waar je die oplost.
func verduidelijk(err error) error {
	if err == nil {
		return nil
	}
	tekst := err.Error()
	switch {
	case strings.Contains(tekst, "Failed to authenticate"),
		strings.Contains(tekst, "OAuth session expired"),
		strings.Contains(tekst, "Invalid API key"),
		strings.Contains(tekst, "authentication_error"):
		return fmt.Errorf("de Claude CLI is niet ingelogd: %w\n\nLos het op met één van beide:\n"+
			"- draai `claude login` in een terminal, of\n"+
			"- zet een Anthropic API-key bij Instellingen › AI (die wordt dan als ANTHROPIC_API_KEY meegegeven)", err)
	case strings.Contains(tekst, "executable file not found"), strings.Contains(tekst, "no such file or directory"):
		return fmt.Errorf("de Claude CLI is niet gevonden (gezocht als %q): %w\n\n"+
			"Installeer die of zet het pad in de omgevingsvariabele RDM_CLAUDE_BIN", Bin(), err)
	}
	return err
}

// Omgeving vult ANTHROPIC_API_KEY aan zonder een bestaande waarde te
// overschrijven, en zet desgevraagd de PATH voor de agent.
func Omgeving(basis []string, apiKey, pad string) []string {
	uit := append([]string{}, basis...)

	if p := strings.TrimSpace(pad); p != "" {
		vervangen := false
		for i, kv := range uit {
			if strings.HasPrefix(kv, "PATH=") {
				uit[i] = "PATH=" + p
				vervangen = true
				break
			}
		}
		if !vervangen {
			uit = append(uit, "PATH="+p)
		}
	}

	if strings.TrimSpace(apiKey) == "" {
		return uit
	}
	for _, kv := range uit {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") && kv != "ANTHROPIC_API_KEY=" {
			return uit
		}
	}
	return append(uit, "ANTHROPIC_API_KEY="+apiKey)
}

// Args bouwt de argumentenlijst. Apart en puur, zodat een test kan controleren
// wat er precies aangeroepen wordt.
func Args(prompt string, o Opties) []string {
	args := []string{
		"-p", prompt,
		// stream-json geeft voortgang tijdens de run; de CLI eist daarbij
		// --verbose, anders weigert hij te starten.
		"--output-format", "stream-json",
		"--verbose",
		// acceptEdits laat de agent bestanden aanpassen zonder te vragen — er is
		// hier geen mens om te vragen — maar houdt de rem op de rest. Bewust
		// niet --dangerously-skip-permissions.
		"--permission-mode", "acceptEdits",
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if len(o.ToegestaneTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(o.ToegestaneTools, ","))
	}
	if len(o.VerbodenTools) > 0 {
		args = append(args, "--disallowedTools", strings.Join(o.VerbodenTools, ","))
	}
	return args
}

// streamRegel is het deel van de stream-json-uitvoer dat we gebruiken.
type streamRegel struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
	Message struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Name string `json:"name"`
		} `json:"content"`
	} `json:"message"`
}

// LeesStream verwerkt de stream-json-uitvoer regel voor regel.
func LeesStream(r io.Reader, onProgress func(string)) (Resultaat, error) {
	var res Resultaat
	var ruw strings.Builder

	sc := bufio.NewScanner(r)
	// Een enkel bericht kan groot zijn (een heel bestand in een tool-result),
	// dus de standaardbuffer van 64 KB is te klein.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	for sc.Scan() {
		regel := sc.Bytes()
		ruw.Write(regel)
		ruw.WriteByte('\n')

		var s streamRegel
		if err := json.Unmarshal(regel, &s); err != nil {
			// Niet elke regel hoeft JSON te zijn; overslaan is beter dan stoppen.
			continue
		}
		switch s.Type {
		case "assistant":
			res.Turns++
			for _, c := range s.Message.Content {
				switch c.Type {
				case "text":
					if t := strings.TrimSpace(c.Text); t != "" {
						res.Samenvatting = t
						meld(onProgress, kapMelding(t))
					}
				case "tool_use":
					if c.Name != "" {
						res.Gereedschappen = append(res.Gereedschappen, c.Name)
						meld(onProgress, c.Name)
					}
				}
			}
		case "result":
			if s.Result != "" {
				res.Samenvatting = strings.TrimSpace(s.Result)
			}
			if s.IsError {
				res.Ruw = ruw.String()
				return res, fmt.Errorf("de AI meldde een fout: %s", kapMelding(res.Samenvatting))
			}
		}
	}
	res.Ruw = ruw.String()
	return res, sc.Err()
}

func meld(onProgress func(string), tekst string) {
	if onProgress != nil && tekst != "" {
		onProgress(tekst)
	}
}

func kapMelding(s string) string {
	s = strings.TrimSpace(s)
	const max = 400
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
