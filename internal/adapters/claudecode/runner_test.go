package claudecode

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestArgsBevatDeVerplichteVlaggen(t *testing.T) {
	args := Args("doe iets", Opties{Dir: "/tmp", Model: "sonnet"})
	samen := strings.Join(args, " ")

	// stream-json zonder --verbose weigert de CLI te starten met
	// "When using --print, --output-format=stream-json requires --verbose".
	if !strings.Contains(samen, "--output-format stream-json") || !strings.Contains(samen, "--verbose") {
		t.Errorf("stream-json vereist --verbose: %v", args)
	}
	if !strings.Contains(samen, "-p doe iets") {
		t.Errorf("prompt ontbreekt: %v", args)
	}
	if !strings.Contains(samen, "--permission-mode acceptEdits") {
		t.Errorf("permission-mode ontbreekt: %v", args)
	}
	if !strings.Contains(samen, "--model sonnet") {
		t.Errorf("model ontbreekt: %v", args)
	}
}

// TestArgsGebruiktNooitPermissieBypass is een vangrail: acceptEdits laat de agent
// bestanden aanpassen, maar bypassPermissions zou hem ook vrij laten in shell en
// netwerk. Dat hoort deze tool nooit te doen.
func TestArgsGebruiktNooitPermissieBypass(t *testing.T) {
	args := Args("x", Opties{Dir: "/tmp", ToegestaneTools: []string{"Read", "Edit"}})
	for _, arg := range args {
		for _, verboden := range []string{"--dangerously-skip-permissions", "--allow-dangerously-skip-permissions", "bypassPermissions"} {
			if strings.Contains(arg, verboden) {
				t.Errorf("argument %q bevat %q", arg, verboden)
			}
		}
	}
}

func TestArgsToolsAlleenAlsGevraagd(t *testing.T) {
	zonder := strings.Join(Args("x", Opties{Dir: "/tmp"}), " ")
	if strings.Contains(zonder, "--allowedTools") || strings.Contains(zonder, "--disallowedTools") {
		t.Errorf("zonder tools horen de vlaggen weg te blijven: %s", zonder)
	}
	met := strings.Join(Args("x", Opties{Dir: "/tmp",
		ToegestaneTools: []string{"Read", "Edit"},
		VerbodenTools:   []string{"WebFetch"},
	}), " ")
	if !strings.Contains(met, "--allowedTools Read,Edit") {
		t.Errorf("allowedTools verkeerd: %s", met)
	}
	if !strings.Contains(met, "--disallowedTools WebFetch") {
		t.Errorf("disallowedTools verkeerd: %s", met)
	}
}

// De stream hieronder is de echte vorm die de CLI (2.1.209) uitstuurt.
const echteStream = `{"type":"system","subtype":"init","session_id":"abc","cwd":"/tmp"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Ik ga het bestand lezen."}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read"}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Edit"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Klaar: null-check toegevoegd."}]}}
{"type":"result","subtype":"success","is_error":false,"result":"Klaar: null-check toegevoegd."}
`

func TestLeesStream(t *testing.T) {
	var voortgang []string
	res, err := LeesStream(strings.NewReader(echteStream), func(s string) { voortgang = append(voortgang, s) })
	if err != nil {
		t.Fatalf("LeesStream: %v", err)
	}
	if res.Samenvatting != "Klaar: null-check toegevoegd." {
		t.Errorf("samenvatting = %q", res.Samenvatting)
	}
	if len(res.Gereedschappen) != 2 || res.Gereedschappen[0] != "Read" || res.Gereedschappen[1] != "Edit" {
		t.Errorf("gereedschappen = %v", res.Gereedschappen)
	}
	if res.Turns != 4 {
		t.Errorf("turns = %d, wil 4", res.Turns)
	}
	if len(voortgang) == 0 {
		t.Error("geen voortgang gemeld")
	}
	if !strings.Contains(res.Ruw, `"type":"result"`) {
		t.Error("ruwe stream niet bewaard")
	}
}

// De CLI stuurt bij een mislukte run subtype "success" mét is_error true. Alleen
// op subtype sturen zou een fout dus als geslaagd doorlaten.
func TestLeesStreamZietIsErrorOndanksSubtypeSuccess(t *testing.T) {
	stream := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Failed to authenticate: OAuth session expired"}]}}
{"type":"result","subtype":"success","is_error":true,"result":"Failed to authenticate: OAuth session expired and could not be refreshed"}
`
	_, err := LeesStream(strings.NewReader(stream), nil)
	if err == nil {
		t.Fatal("verwachtte een fout bij is_error true")
	}
	if !strings.Contains(err.Error(), "OAuth session expired") {
		t.Errorf("fout = %v", err)
	}
}

func TestLeesStreamNegeertNietJSON(t *testing.T) {
	stream := "npm warn: iets\n" + echteStream
	res, err := LeesStream(strings.NewReader(stream), nil)
	if err != nil {
		t.Fatalf("LeesStream: %v", err)
	}
	if res.Samenvatting == "" {
		t.Error("een niet-JSON regel hoort de rest niet te blokkeren")
	}
}

func TestLeesStreamVerwerktGroteRegels(t *testing.T) {
	// Een tool_result met een heel bestand erin overschrijdt de standaardbuffer
	// van bufio.Scanner (64 KB).
	groot := strings.Repeat("a", 200_000)
	stream := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"` + groot + `"}]}}` + "\n" +
		`{"type":"result","subtype":"success","is_error":false,"result":"klaar"}` + "\n"
	res, err := LeesStream(strings.NewReader(stream), nil)
	if err != nil {
		t.Fatalf("LeesStream: %v", err)
	}
	if res.Samenvatting != "klaar" {
		t.Errorf("samenvatting = %q", res.Samenvatting)
	}
}

func TestOmgevingVultAPIKeyAan(t *testing.T) {
	uit := Omgeving([]string{"PATH=/bin"}, "sk-test", "")
	if len(uit) != 2 || uit[1] != "ANTHROPIC_API_KEY=sk-test" {
		t.Errorf("omgeving = %v", uit)
	}
}

func TestOmgevingOverschrijftBestaandeKeyNiet(t *testing.T) {
	basis := []string{"PATH=/bin", "ANTHROPIC_API_KEY=van-de-gebruiker"}
	uit := Omgeving(basis, "sk-uit-de-config", "")
	for _, kv := range uit {
		if kv == "ANTHROPIC_API_KEY=sk-uit-de-config" {
			t.Error("bestaande ANTHROPIC_API_KEY werd overschreven")
		}
	}
}

func TestOmgevingZonderKeyBlijftGelijk(t *testing.T) {
	basis := []string{"PATH=/bin"}
	if uit := Omgeving(basis, "  ", ""); len(uit) != 1 {
		t.Errorf("omgeving = %v", uit)
	}
}

func TestRunWeigertLegeOpdrachtEnWerkmap(t *testing.T) {
	if _, err := Run(context.Background(), "  ", Opties{Dir: "/tmp"}); err == nil {
		t.Error("lege opdracht werd geaccepteerd")
	}
	if _, err := Run(context.Background(), "x", Opties{}); err == nil {
		t.Error("ontbrekende werkmap werd geaccepteerd")
	}
	if _, err := Run(context.Background(), "x", Opties{Dir: "/bestaat/echt/niet"}); err == nil {
		t.Error("niet-bestaande werkmap werd geaccepteerd")
	}
}

func TestRunLeestStreamVanEenNepBinary(t *testing.T) {
	origineel := execCommandContext
	t.Cleanup(func() { execCommandContext = origineel })
	execCommandContext = func(ctx context.Context, naam string, args ...string) *exec.Cmd {
		// Doe alsof claude de stream uitspuugt.
		return exec.CommandContext(ctx, "printf", "%s", echteStream)
	}

	res, err := Run(context.Background(), "doe iets", Opties{Dir: os.TempDir()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Samenvatting != "Klaar: null-check toegevoegd." {
		t.Errorf("samenvatting = %q", res.Samenvatting)
	}
}

func TestVerduidelijktAuthfout(t *testing.T) {
	err := verduidelijk(errFouteAuth{})
	if err == nil || !strings.Contains(err.Error(), "claude login") {
		t.Errorf("fout mist de oplossing: %v", err)
	}
}

type errFouteAuth struct{}

func (errFouteAuth) Error() string {
	return "claude eindigde met een fout: exit status 1: Failed to authenticate: OAuth session expired"
}

func TestBinValtTerugOpEenNaam(t *testing.T) {
	// Zonder vondst hoort er "claude" uit te komen, niet een leeg pad: dan geeft
	// exec een foutmelding die verduidelijk() kan omzetten in een instructie.
	origineel := claudeLookPath
	t.Cleanup(func() { claudeLookPath = origineel })
	claudeLookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	t.Setenv("RDM_CLAUDE_BIN", "")
	t.Setenv("SHELL", "")
	if got := zoekClaude(); got == "" {
		t.Error("zoekClaude gaf een leeg pad")
	}
}

func TestBinRespecteertOmgevingsvariabele(t *testing.T) {
	t.Setenv("RDM_CLAUDE_BIN", "/eigen/pad/claude")
	if got := zoekClaude(); got != "/eigen/pad/claude" {
		t.Errorf("zoekClaude = %q", got)
	}
}

func TestOmgevingZetPATHVoorDeAgent(t *testing.T) {
	// In de geinstalleerde app is de geerfde PATH uitgekleed; dan moet de agent
	// een aangevulde PATH krijgen, anders vindt hij php/composer/npm niet.
	uit := Omgeving([]string{"PATH=/usr/bin:/bin", "HOME=/x"}, "", "/usr/local/bin:/usr/bin:/bin")
	var gevonden int
	for _, kv := range uit {
		if strings.HasPrefix(kv, "PATH=") {
			gevonden++
			if kv != "PATH=/usr/local/bin:/usr/bin:/bin" {
				t.Errorf("PATH = %q", kv)
			}
		}
	}
	if gevonden != 1 {
		t.Errorf("PATH komt %d keer voor: %v", gevonden, uit)
	}
}

func TestOmgevingLaatPATHStaanZonderOpgave(t *testing.T) {
	uit := Omgeving([]string{"PATH=/usr/bin"}, "", "")
	if uit[0] != "PATH=/usr/bin" {
		t.Errorf("PATH onterecht gewijzigd: %v", uit)
	}
}
