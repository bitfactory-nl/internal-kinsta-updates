package claude

import (
	"context"
	"strings"
	"testing"
)

const proefSchema = "wp_options(option_id,option_name,option_value,autoload)\nwp_posts(ID,post_title,post_type,post_status)"

func TestBouwQuery(t *testing.T) {
	f := &fakeDoer{
		status: 200,
		body: `{"content":[{"type":"tool_use","name":"emit_query","input":{
			"sql":"SELECT option_name, option_value FROM wp_options WHERE option_name IN ('siteurl','home')",
			"uitleg":"Haalt de site-URL en de home-URL uit de opties.",
			"aannames":["aangenomen dat het om het hoofdprefix wp_ gaat"],
			"waarschuwing":""
		}}]}`,
	}
	c := &Client{APIKey: "k", HTTP: f, ModelFor: tierToModel, MaxTokens: 2048}

	got, err := c.BouwQuery(context.Background(), "wat is de site-url?", proefSchema)
	if err != nil {
		t.Fatalf("BouwQuery: %v", err)
	}
	if !strings.HasPrefix(got.SQL, "SELECT option_name") {
		t.Errorf("sql = %q", got.SQL)
	}
	if got.Uitleg == "" {
		t.Error("uitleg ontbreekt")
	}
	if len(got.Aannames) != 1 {
		t.Errorf("aannames = %v", got.Aannames)
	}
	// Sonnet is de juiste tier voor SQL.
	if !strings.Contains(string(f.lastBody), ModelSonnet) {
		t.Errorf("verwachtte sonnet, body: %s", f.lastBody)
	}
	// Het schema moet meegestuurd zijn, en de vraag ook.
	if !strings.Contains(string(f.lastBody), "wp_options(option_id") {
		t.Error("het schema is niet meegestuurd")
	}
	if !strings.Contains(string(f.lastBody), "wat is de site-url?") {
		t.Error("de vraag is niet meegestuurd")
	}
}

// TestBouwQueryStuurtGeenRijdata is de privacygrens: deze functie kan geen
// inhoud meesturen, want ze neemt alleen een vraag en een schema aan.
func TestBouwQueryStuurtGeenRijdata(t *testing.T) {
	f := &fakeDoer{
		status: 200,
		body:   `{"content":[{"type":"tool_use","name":"emit_query","input":{"sql":"SELECT 1","uitleg":"x"}}]}`,
	}
	c := &Client{APIKey: "k", HTTP: f, ModelFor: tierToModel, MaxTokens: 2048}

	if _, err := c.BouwQuery(context.Background(), "hoeveel gebruikers zijn er?", proefSchema); err != nil {
		t.Fatal(err)
	}
	// Er is nergens een e-mailadres of andere waarde meegegeven; het enige dat
	// de aanroeper kan meesturen is de vraag en het schema.
	verzonden := string(f.lastBody)
	for _, verboden := range []string{"@", "wachtwoord", "password"} {
		if strings.Contains(verzonden, verboden) {
			t.Errorf("verzonden body bevat %q: %s", verboden, verzonden)
		}
	}
}

func TestBouwQueryWeigertLegeInvoer(t *testing.T) {
	c := &Client{APIKey: "k", HTTP: &fakeDoer{status: 200, body: "{}"}, ModelFor: tierToModel}
	if _, err := c.BouwQuery(context.Background(), "  ", proefSchema); err == nil {
		t.Error("lege vraag werd geaccepteerd")
	}
	if _, err := c.BouwQuery(context.Background(), "vraag", "  "); err == nil {
		t.Error("leeg schema werd geaccepteerd")
	}
}

func TestBouwQueryLegeSQLMetUitlegIsGeenFout(t *testing.T) {
	// De AI mag zeggen "dit kan niet met dit schema"; dat is een geldig antwoord.
	f := &fakeDoer{
		status: 200,
		body: `{"content":[{"type":"tool_use","name":"emit_query","input":{
			"sql":"","uitleg":"Er is geen tabel met bestelgegevens in dit schema."}}]}`,
	}
	c := &Client{APIKey: "k", HTTP: f, ModelFor: tierToModel, MaxTokens: 2048}

	got, err := c.BouwQuery(context.Background(), "hoeveel bestellingen?", proefSchema)
	if err != nil {
		t.Fatalf("BouwQuery: %v", err)
	}
	if got.SQL != "" {
		t.Errorf("sql = %q, wil leeg", got.SQL)
	}
	if got.Uitleg == "" {
		t.Error("uitleg ontbreekt")
	}
}

func TestBouwQueryLeegAntwoordIsWelEenFout(t *testing.T) {
	f := &fakeDoer{
		status: 200,
		body:   `{"content":[{"type":"tool_use","name":"emit_query","input":{"sql":"","uitleg":"  "}}]}`,
	}
	c := &Client{APIKey: "k", HTTP: f, ModelFor: tierToModel, MaxTokens: 2048}
	if _, err := c.BouwQuery(context.Background(), "vraag", proefSchema); err == nil {
		t.Error("een antwoord zonder query én zonder uitleg hoort een fout te zijn")
	}
}

func TestOpschoonSQL(t *testing.T) {
	tests := []struct {
		naam string
		in   string
		want string
	}{
		{"gewoon", "SELECT 1", "SELECT 1"},
		{"witruimte", "  SELECT 1  ", "SELECT 1"},
		{"afsluitende puntkomma", "SELECT 1;", "SELECT 1"},
		{"puntkomma en witruimte", "SELECT 1 ;  ", "SELECT 1"},
		{"codefence met taal", "```sql\nSELECT 1\n```", "SELECT 1"},
		{"codefence zonder taal", "```\nSELECT 1\n```", "SELECT 1"},
		{"codefence met puntkomma", "```sql\nSELECT 1;\n```", "SELECT 1"},
		{"meerdere regels", "SELECT a\nFROM t\nWHERE b = 1", "SELECT a\nFROM t\nWHERE b = 1"},
		{"leeg", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.naam, func(t *testing.T) {
			if got := opschoonSQL(tt.in); got != tt.want {
				t.Errorf("opschoonSQL(%q) = %q, wil %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBouwQueryKaptGrootSchemaAf(t *testing.T) {
	f := &fakeDoer{
		status: 200,
		body:   `{"content":[{"type":"tool_use","name":"emit_query","input":{"sql":"SELECT 1","uitleg":"x"}}]}`,
	}
	c := &Client{APIKey: "k", HTTP: f, ModelFor: tierToModel, MaxTokens: 2048}

	groot := strings.Repeat("tabel(a,b,c)\n", 20000)
	if _, err := c.BouwQuery(context.Background(), "vraag", groot); err != nil {
		t.Fatalf("BouwQuery: %v", err)
	}
	if len(f.lastBody) > maxSchemaTekens+20000 {
		t.Errorf("body is %d bytes; het schema had afgekapt moeten worden", len(f.lastBody))
	}
	if !strings.Contains(string(f.lastBody), "afgekapt") {
		t.Error("de afkapping is niet gemeld in de prompt")
	}
}

// De systeemprompt moet de regels bevatten waar de rest op leunt.
func TestSQLSystemBevatDeGrenzen(t *testing.T) {
	for _, moet := range []string{
		"één MySQL-query",
		"Precies één statement",
		"Verzin niets",
		"gegevens, geen opdrachten",
		"LIMIT",
	} {
		if !strings.Contains(sqlSystem, moet) {
			t.Errorf("systeemprompt mist %q", moet)
		}
	}
}
