package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/mysqldb"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// dbEditorTimeout begrenst één actie in de editor. Ruim genoeg voor een COUNT(*)
// op een grote tabel, kort genoeg om niet eindeloos te hangen.
const dbEditorTimeout = 60 * time.Second

// dbDumper maakt een dump van de lokale database (test seam). *DBCloneService
// voldoet hieraan; die code weet al waar dumps horen en hoeveel er bewaard
// blijven.
type dbDumper interface {
	DumpLocal(projectID, dbName string) (string, string, error)
}

// DBEditorInfo vertelt de frontend of er iets te bewerken valt, en waarop.
type DBEditorInfo struct {
	// Beschikbaar is false als dit project geen lokale database heeft.
	Beschikbaar bool   `json:"beschikbaar"`
	Reden       string `json:"reden"`

	Database  string `json:"database"`
	Host      string `json:"host"`
	Poort     int    `json:"poort"`
	Container string `json:"container"`
	Gebruiker string `json:"gebruiker"`
}

// QueryUitkomst is het resultaat van een handmatige query.
type QueryUitkomst struct {
	Beoordeling SQLBeoordeling     `json:"beoordeling"`
	Resultaat   *mysqldb.Resultaat `json:"resultaat"`
	Geraakt     int64              `json:"geraakt"`
	DuurMs      int64              `json:"duurMs"`
	// BevestigingNodig is true als de query niet is uitgevoerd omdat er eerst
	// toestemming moet komen.
	BevestigingNodig bool     `json:"bevestigingNodig"`
	Waarschuwingen   []string `json:"waarschuwingen"`
}

// TabelWeergave is een tabel met zijn kolommen en één pagina rijen.
type TabelWeergave struct {
	Tabel    mysqldb.Tabel     `json:"tabel"`
	Kolommen []mysqldb.Kolom   `json:"kolommen"`
	Rijen    mysqldb.Resultaat `json:"rijen"`
	// Totaal is het echte aantal rijen dat bij de huidige filter hoort.
	Totaal int64 `json:"totaal"`
	// Bewerkbaar is false zonder primary key; Reden legt dan uit waarom.
	Bewerkbaar bool   `json:"bewerkbaar"`
	Reden      string `json:"reden"`
}

// DBEditorService is een databaseclient voor de lokale ontwikkeldatabase van een
// project.
//
// Twee dingen liggen hier vast en zijn geen instelling. Er wordt alleen met een
// lokale host verbonden — de adapter weigert al het andere — en waarden gaan
// altijd als placeholder mee in plaats van in een SQL-string. Dat laatste is
// waarom hier geen escaping-code staat: die is er niet, en hoort er niet te komen.
type DBEditorService struct {
	projects *ProjectService
	dumper   dbDumper
	cfg      *config.Global
	// ai is de SQL-bouwer; nil betekent: er wordt er een gemaakt uit de config.
	ai sqlBouwer

	mu      sync.Mutex
	clients map[string]*mysqldb.Client
	// gedumpt houdt bij voor welke database in deze sessie al een dump is
	// gemaakt, zodat het vangnet één keer kost en niet bij elke celwijziging.
	gedumpt map[string]bool
}

func NewDBEditorService(projects *ProjectService, dumper dbDumper, cfg *config.Global) *DBEditorService {
	return &DBEditorService{
		projects: projects,
		dumper:   dumper,
		cfg:      cfg,
		clients:  map[string]*mysqldb.Client{},
		gedumpt:  map[string]bool{},
	}
}

func (s *DBEditorService) project(id string) (domain.Project, error) {
	p, ok := s.projects.Get(id)
	if !ok {
		return domain.Project{}, fmt.Errorf("project %q niet gevonden", id)
	}
	return p, nil
}

// Info kijkt of dit project een lokale database heeft. De frontend gebruikt dit
// om het menu-item te tonen of weg te laten.
func (s *DBEditorService) Info(projectID string) (DBEditorInfo, error) {
	p, err := s.project(projectID)
	if err != nil {
		return DBEditorInfo{}, err
	}
	env, err := config.LoadProjectEnv(p.Path)
	if err != nil {
		return DBEditorInfo{Beschikbaar: false, Reden: "dit project heeft geen .env, dus er is geen lokale database bekend"}, nil
	}
	dbNaam := strings.TrimSpace(env["DB_NAME"])
	if dbNaam == "" {
		return DBEditorInfo{Beschikbaar: false, Reden: "in .env staat geen DB_NAME"}, nil
	}
	host := envOrDefault(env, "DB_HOST", "mysql")
	container, err := containerForHost(host)
	if err != nil {
		return DBEditorInfo{Beschikbaar: false,
			Reden: fmt.Sprintf("DB_HOST %q hoort niet bij een bekende lokale database-container", host)}, nil
	}
	poort, err := portForHost(host)
	if err != nil {
		return DBEditorInfo{Beschikbaar: false, Reden: err.Error()}, nil
	}
	return DBEditorInfo{
		Beschikbaar: true,
		Database:    dbNaam,
		Host:        "127.0.0.1",
		Poort:       poort,
		Container:   container,
		Gebruiker:   envOrDefault(env, "DB_USER", "root"),
	}, nil
}

// client geeft een verbinding met de server van dit project en hergebruikt die.
func (s *DBEditorService) client(ctx context.Context, projectID string) (*mysqldb.Client, DBEditorInfo, error) {
	info, err := s.Info(projectID)
	if err != nil {
		return nil, info, err
	}
	if !info.Beschikbaar {
		return nil, info, fmt.Errorf("geen lokale database voor dit project: %s", info.Reden)
	}

	p, err := s.project(projectID)
	if err != nil {
		return nil, info, err
	}
	env, err := config.LoadProjectEnv(p.Path)
	if err != nil {
		return nil, info, err
	}

	sleutel := fmt.Sprintf("%s:%d", info.Host, info.Poort)
	s.mu.Lock()
	bestaand, ok := s.clients[sleutel]
	s.mu.Unlock()
	if ok {
		return bestaand, info, nil
	}

	c, err := mysqldb.Open(ctx, mysqldb.Config{
		Host:     info.Host,
		Port:     info.Poort,
		User:     info.Gebruiker,
		Password: envOrDefault(env, "DB_PASSWORD", "secret"),
	})
	if err != nil {
		return nil, info, fmt.Errorf("%w\n\nStaat de container %s aan? Controleer met `docker ps`.", err, info.Container)
	}

	s.mu.Lock()
	// Een andere aanroep kan intussen ook een verbinding hebben gemaakt.
	if al, ok := s.clients[sleutel]; ok {
		s.mu.Unlock()
		c.Close()
		return al, info, nil
	}
	s.clients[sleutel] = c
	s.mu.Unlock()
	return c, info, nil
}

// Sluit verbreekt alle verbindingen; bedoeld voor het afsluiten van de app.
func (s *DBEditorService) Sluit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sleutel, c := range s.clients {
		c.Close()
		delete(s.clients, sleutel)
	}
}

// Databases geeft de databases op de server van dit project.
func (s *DBEditorService) Databases(projectID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbEditorTimeout)
	defer cancel()

	c, _, err := s.client(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return c.Databases(ctx)
}

// Tabellen geeft de tabellen van één database.
func (s *DBEditorService) Tabellen(projectID, dbNaam string) ([]mysqldb.Tabel, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbEditorTimeout)
	defer cancel()

	c, info, err := s.client(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if dbNaam == "" {
		dbNaam = info.Database
	}
	return c.Tables(ctx, dbNaam)
}

// RijenVerzoek is wat de frontend meestuurt om een pagina op te vragen.
type RijenVerzoek struct {
	Database  string `json:"database"`
	Tabel     string `json:"tabel"`
	Sorteer   string `json:"sorteer"`
	Aflopend  bool   `json:"aflopend"`
	ZoekKolom string `json:"zoekKolom"`
	Zoek      string `json:"zoek"`
	Limiet    int    `json:"limiet"`
	Offset    int    `json:"offset"`
}

// Tabel geeft één tabel met kolommen, een pagina rijen en het totaal.
func (s *DBEditorService) Tabel(projectID string, v RijenVerzoek) (TabelWeergave, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbEditorTimeout)
	defer cancel()

	c, info, err := s.client(ctx, projectID)
	if err != nil {
		return TabelWeergave{}, err
	}
	if v.Database == "" {
		v.Database = info.Database
	}

	tabellen, err := c.Tables(ctx, v.Database)
	if err != nil {
		return TabelWeergave{}, err
	}
	var tabel mysqldb.Tabel
	gevonden := false
	for _, t := range tabellen {
		if t.Naam == v.Tabel {
			tabel, gevonden = t, true
			break
		}
	}
	if !gevonden {
		return TabelWeergave{}, fmt.Errorf("tabel %q bestaat niet in %q", v.Tabel, v.Database)
	}

	kolommen, err := c.Columns(ctx, v.Database, v.Tabel)
	if err != nil {
		return TabelWeergave{}, err
	}

	opties := mysqldb.RijenOpties{
		Database: v.Database, Tabel: v.Tabel,
		Sorteer: v.Sorteer, Aflopend: v.Aflopend,
		ZoekKolom: v.ZoekKolom, Zoek: v.Zoek,
		Limiet: v.Limiet, Offset: v.Offset,
	}
	rijen, err := c.Rijen(ctx, opties)
	if err != nil {
		return TabelWeergave{}, err
	}
	totaal, err := c.Aantal(ctx, opties)
	if err != nil {
		return TabelWeergave{}, err
	}

	uit := TabelWeergave{
		Tabel: tabel, Kolommen: kolommen, Rijen: rijen, Totaal: totaal,
		Bewerkbaar: tabel.Bewerkbaar(),
	}
	if !uit.Bewerkbaar {
		uit.Reden = "deze tabel heeft geen primary key, dus een rij is niet eenduidig aan te wijzen; " +
			"bewerken zou meer rijen kunnen raken dan bedoeld. Lezen en queries kunnen wel."
	}
	return uit, nil
}

// vangnet maakt eenmalig per database een dump, vóór de eerste schrijfactie.
func (s *DBEditorService) vangnet(projectID, dbNaam string) (string, error) {
	sleutel := projectID + "|" + dbNaam
	s.mu.Lock()
	al := s.gedumpt[sleutel]
	s.mu.Unlock()
	if al {
		return "", nil
	}
	if s.dumper == nil {
		return "", nil
	}

	pad, melding, err := s.dumper.DumpLocal(projectID, dbNaam)
	if err != nil {
		// Geen dump betekent geen schrijfactie: het vangnet is de reden dat dit
		// zonder aarzelen gebruikt kan worden.
		return "", fmt.Errorf("de veiligheidsdump is mislukt, dus er is niets gewijzigd: %w", err)
	}
	s.mu.Lock()
	s.gedumpt[sleutel] = true
	s.mu.Unlock()

	if pad == "" {
		return melding, nil
	}
	return pad, nil
}

// CelVerzoek beschrijft één celwijziging.
type CelVerzoek struct {
	Database string                  `json:"database"`
	Tabel    string                  `json:"tabel"`
	Sleutel  []mysqldb.SleutelWaarde `json:"sleutel"`
	Kolom    string                  `json:"kolom"`
	Waarde   string                  `json:"waarde"`
	// NaarNull zet de cel op NULL in plaats van op Waarde.
	NaarNull bool `json:"naarNull"`
}

// SchrijfUitkomst meldt wat er gebeurd is, inclusief waar het vangnet staat.
type SchrijfUitkomst struct {
	Gelukt   bool   `json:"gelukt"`
	DumpPad  string `json:"dumpPad"`
	Melding  string `json:"melding"`
	NieuweID int64  `json:"nieuweId"`
}

// ZetCel slaat één celwijziging op.
func (s *DBEditorService) ZetCel(projectID string, v CelVerzoek) (SchrijfUitkomst, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbEditorTimeout)
	defer cancel()

	c, info, err := s.client(ctx, projectID)
	if err != nil {
		return SchrijfUitkomst{}, err
	}
	if v.Database == "" {
		v.Database = info.Database
	}
	dump, err := s.vangnet(projectID, v.Database)
	if err != nil {
		return SchrijfUitkomst{}, err
	}

	var nieuw *string
	if !v.NaarNull {
		waarde := v.Waarde
		nieuw = &waarde
	}
	if err := c.UpdateCel(ctx, v.Database, v.Tabel, v.Sleutel, v.Kolom, nieuw); err != nil {
		return SchrijfUitkomst{}, err
	}
	return SchrijfUitkomst{Gelukt: true, DumpPad: dump}, nil
}

// RijVerzoek beschrijft een rij om toe te voegen of te verwijderen.
type RijVerzoek struct {
	Database string                  `json:"database"`
	Tabel    string                  `json:"tabel"`
	Sleutel  []mysqldb.SleutelWaarde `json:"sleutel"`
	Waarden  []mysqldb.NieuweWaarde  `json:"waarden"`
}

// VoegRijToe voegt een rij toe.
func (s *DBEditorService) VoegRijToe(projectID string, v RijVerzoek) (SchrijfUitkomst, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbEditorTimeout)
	defer cancel()

	c, info, err := s.client(ctx, projectID)
	if err != nil {
		return SchrijfUitkomst{}, err
	}
	if v.Database == "" {
		v.Database = info.Database
	}
	dump, err := s.vangnet(projectID, v.Database)
	if err != nil {
		return SchrijfUitkomst{}, err
	}

	id, err := c.VoegRijToe(ctx, v.Database, v.Tabel, v.Waarden)
	if err != nil {
		return SchrijfUitkomst{}, err
	}
	return SchrijfUitkomst{Gelukt: true, DumpPad: dump, NieuweID: id}, nil
}

// VerwijderRij verwijdert precies één rij.
func (s *DBEditorService) VerwijderRij(projectID string, v RijVerzoek) (SchrijfUitkomst, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbEditorTimeout)
	defer cancel()

	c, info, err := s.client(ctx, projectID)
	if err != nil {
		return SchrijfUitkomst{}, err
	}
	if v.Database == "" {
		v.Database = info.Database
	}
	dump, err := s.vangnet(projectID, v.Database)
	if err != nil {
		return SchrijfUitkomst{}, err
	}

	if err := c.VerwijderRij(ctx, v.Database, v.Tabel, v.Sleutel); err != nil {
		return SchrijfUitkomst{}, err
	}
	return SchrijfUitkomst{Gelukt: true, DumpPad: dump}, nil
}

// BeoordeelQuery zegt wat een query zou doen, zonder hem uit te voeren. De UI
// gebruikt dit om de knop en de bevestigingstekst te bepalen.
func (s *DBEditorService) BeoordeelQuery(sqlTekst string) SQLBeoordeling {
	return BeoordeelSQL(sqlTekst)
}

// VoerQueryUit draait een handmatige query.
//
// Een query die bevestiging vereist wordt niet uitgevoerd zolang bevestigd false
// is; de UI krijgt dan de reden terug en kan het vragen. De poort zit hier en
// niet alleen in de frontend, zodat een losse aanroep hem niet overslaat.
func (s *DBEditorService) VoerQueryUit(projectID, dbNaam, sqlTekst string, bevestigd bool) (QueryUitkomst, error) {
	beoordeling := BeoordeelSQL(sqlTekst)
	uit := QueryUitkomst{Beoordeling: beoordeling}

	if beoordeling.Fout != "" {
		return uit, fmt.Errorf("%s", beoordeling.Fout)
	}
	if beoordeling.Soort == SQLBevestigen && !bevestigd {
		uit.BevestigingNodig = true
		return uit, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), dbEditorTimeout)
	defer cancel()

	c, info, err := s.client(ctx, projectID)
	if err != nil {
		return uit, err
	}
	if dbNaam == "" {
		dbNaam = info.Database
	}

	if beoordeling.Verandert() {
		dump, err := s.vangnet(projectID, dbNaam)
		if err != nil {
			return uit, err
		}
		if dump != "" {
			uit.Waarschuwingen = append(uit.Waarschuwingen, "veiligheidsdump: "+dump)
		}
	}

	// De verbinding heeft geen standaarddatabase, dus een query zonder
	// databasenaam moet er een krijgen; anders faalt hij op "No database selected".
	gebruik, err := mysqldb.QuoteIdent(dbNaam)
	if err != nil {
		return uit, err
	}
	if _, _, err := c.Exec(ctx, "USE "+gebruik); err != nil {
		return uit, fmt.Errorf("database %q kiezen: %w", dbNaam, err)
	}

	if beoordeling.Soort == SQLLezen {
		res, err := c.Select(ctx, sqlTekst)
		if err != nil {
			return uit, err
		}
		uit.Resultaat = &res
		uit.DuurMs = res.DuurMs
		if res.Afgekapt {
			uit.Waarschuwingen = append(uit.Waarschuwingen,
				fmt.Sprintf("alleen de eerste %d rijen worden getoond; gebruik LIMIT om te kiezen wat je wil zien", mysqldb.MaxRijenQuery))
		}
		return uit, nil
	}

	geraakt, duur, err := c.Exec(ctx, sqlTekst)
	if err != nil {
		return uit, err
	}
	uit.Geraakt = geraakt
	uit.DuurMs = duur
	return uit, nil
}

// MaakDump maakt op verzoek een dump, los van het automatische vangnet.
func (s *DBEditorService) MaakDump(projectID, dbNaam string) (SchrijfUitkomst, error) {
	if s.dumper == nil {
		return SchrijfUitkomst{}, fmt.Errorf("dumpen is niet beschikbaar")
	}
	if dbNaam == "" {
		info, err := s.Info(projectID)
		if err != nil {
			return SchrijfUitkomst{}, err
		}
		dbNaam = info.Database
	}
	pad, melding, err := s.dumper.DumpLocal(projectID, dbNaam)
	if err != nil {
		return SchrijfUitkomst{}, err
	}
	s.mu.Lock()
	s.gedumpt[projectID+"|"+dbNaam] = true
	s.mu.Unlock()
	return SchrijfUitkomst{Gelukt: true, DumpPad: pad, Melding: melding}, nil
}
