package services

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/claude"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// aiQueryTimeout begrenst één AI-aanroep.
const aiQueryTimeout = 90 * time.Second

// sqlBouwer zet een vraag om in een query (test seam).
type sqlBouwer interface {
	BouwQuery(ctx context.Context, vraag, schema string) (domain.AISQLAntwoord, error)
}

// AIQueryVoorstel is een voorstel van de AI. Er is niets uitgevoerd: dat gebeurt
// pas als de gebruiker apart op uitvoeren klikt, en dan door dezelfde poort als
// een handmatig getypte query.
type AIQueryVoorstel struct {
	Vraag string `json:"vraag"`

	SQL          string   `json:"sql"`
	Uitleg       string   `json:"uitleg"`
	Aannames     []string `json:"aannames"`
	Waarschuwing string   `json:"waarschuwing"`

	// Beoordeling is ons eigen oordeel over de SQL die de AI gaf, met dezelfde
	// regels als voor een getypte query. De UI kan hiermee vooraf laten zien dat
	// uitvoeren om bevestiging gaat vragen.
	Beoordeling SQLBeoordeling `json:"beoordeling"`

	// Tabellen zijn de tabellen uit deze database die in de query voorkomen.
	Tabellen []string `json:"tabellen"`

	// Waarschuwingen komen van de tool zelf, niet van de AI.
	Waarschuwingen []string `json:"waarschuwingen"`
}

// bouwer geeft de AI-client, of een duidelijke fout als er geen key is.
func (s *DBEditorService) bouwer() (sqlBouwer, error) {
	if s.ai != nil {
		return s.ai, nil
	}
	if s.cfg == nil {
		return nil, fmt.Errorf("configuratie niet beschikbaar")
	}
	key, err := config.ResolveSecret(s.cfg.AI.APIKey)
	if err != nil {
		return nil, fmt.Errorf("anthropic api key: %w", err)
	}
	if key == "" {
		return nil, fmt.Errorf("er is geen Anthropic API-key geconfigureerd; vul die in bij Instellingen")
	}
	c := claude.NewClient(key)
	// Een query met uitleg en aannames past niet in de standaard 1024 tokens.
	c.MaxTokens = 4096
	return c, nil
}

// BouwQuery zet een vraag in natuurlijke taal om in een query, zonder die uit te
// voeren.
//
// Naar de AI gaat alleen het schema: tabelnamen, kolomnamen en types. Er gaat
// geen enkele rij mee. Dat is belangrijk omdat een gekloonde klantdatabase
// persoonsgegevens bevat; het schema zegt niets over personen.
func (s *DBEditorService) BouwQuery(projectID, dbNaam, vraag string) (AIQueryVoorstel, error) {
	if strings.TrimSpace(vraag) == "" {
		return AIQueryVoorstel{}, fmt.Errorf("stel eerst een vraag")
	}
	bouwer, err := s.bouwer()
	if err != nil {
		return AIQueryVoorstel{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), aiQueryTimeout)
	defer cancel()

	c, info, err := s.client(ctx, projectID)
	if err != nil {
		return AIQueryVoorstel{}, err
	}
	if dbNaam == "" {
		dbNaam = info.Database
	}

	schema, tabellen, err := c.SchemaTekst(ctx, dbNaam)
	if err != nil {
		return AIQueryVoorstel{}, err
	}

	antwoord, err := bouwer.BouwQuery(ctx, vraag, schema)
	if err != nil {
		return AIQueryVoorstel{}, fmt.Errorf("de AI kon geen query maken: %w", err)
	}

	voorstel := AIQueryVoorstel{
		Vraag:        vraag,
		SQL:          antwoord.SQL,
		Uitleg:       antwoord.Uitleg,
		Aannames:     antwoord.Aannames,
		Waarschuwing: antwoord.Waarschuwing,
	}
	if voorstel.SQL == "" {
		voorstel.Waarschuwingen = append(voorstel.Waarschuwingen,
			"de AI kon van deze vraag geen query maken; lees de uitleg")
		return voorstel, nil
	}

	// Hetzelfde oordeel als voor een getypte query, zodat de UI vooraf kan
	// zeggen dat uitvoeren om bevestiging gaat vragen.
	voorstel.Beoordeling = BeoordeelSQL(voorstel.SQL)
	if voorstel.Beoordeling.Fout != "" {
		voorstel.Waarschuwingen = append(voorstel.Waarschuwingen,
			"de query die de AI gaf is niet uitvoerbaar: "+voorstel.Beoordeling.Fout)
	}

	voorstel.Tabellen = tabellenInQuery(voorstel.SQL, tabellen)
	if len(voorstel.Tabellen) == 0 {
		voorstel.Waarschuwingen = append(voorstel.Waarschuwingen,
			"deze query noemt geen enkele tabel uit "+dbNaam+"; controleer of hij over de juiste database gaat")
	}
	if voorstel.Beoordeling.Verandert() {
		voorstel.Waarschuwingen = append(voorstel.Waarschuwingen,
			"dit is geen lees-query: uitvoeren wijzigt gegevens, en er wordt eerst een dump gemaakt")
	}
	return voorstel, nil
}

// reWoord vindt losse identifier-achtige woorden.
var reWoord = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_$]*`)

// tabellenInQuery geeft de bekende tabellen die in de query voorkomen.
//
// Er wordt op hele woorden gematcht in de gestripte query, zodat een tabelnaam
// die alleen in een stringwaarde staat niet meetelt en wp_posts niet ook
// wp_postmeta oplevert.
func tabellenInQuery(sqlTekst string, bekend []string) []string {
	kaal := stripCommentaarEnLiteralen(sqlTekst)
	aanwezig := map[string]bool{}
	for _, w := range reWoord.FindAllString(kaal, -1) {
		aanwezig[strings.ToLower(w)] = true
	}
	var uit []string
	for _, t := range bekend {
		if aanwezig[strings.ToLower(t)] {
			uit = append(uit, t)
		}
	}
	return uit
}
