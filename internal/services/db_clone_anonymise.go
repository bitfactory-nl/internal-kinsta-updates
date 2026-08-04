package services

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
)

// InspectSensitiveData reads the production database's table list and reports
// which tables hold personal data, which roles exist and how many users there
// are. Read-only: this only looks.
func (s *DBCloneService) InspectSensitiveData(projectID, envID string) (domain.SensitiveDataReport, error) {
	tgt, _, err := s.target(projectID, envID)
	if err != nil {
		return domain.SensitiveDataReport{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), dbProbeTimeout)
	defer cancel()

	out, err := s.ssh.RunCommand(ctx, tgt.SSH, buildTableInventoryCommand(tgt.Webroot))
	if melding := eersteGroep(reMediaErr, out); melding != "" {
		return domain.SensitiveDataReport{}, fmt.Errorf("op de server: %s", melding)
	}
	inventaris := parseTableInventory(out)
	if err != nil && len(inventaris) == 0 {
		return domain.SensitiveDataReport{}, fmt.Errorf("tabellen ophalen van %s: %w", tgt.SSH.Host, err)
	}

	prefix := sanitizeTablePrefix(eersteGroep(reDBPrefix, out))
	rapport := domain.SensitiveDataReport{
		TablePrefix: prefix,
		Tables:      vindGevoeligeTabellen(inventaris, prefix),
	}
	for _, r := range inventaris {
		rapport.AllTables = append(rapport.AllTables, r.Naam)
		if stripTabelPrefix(r.Naam, prefix) == "users" {
			rapport.UserCount = r.Rijen
		}
	}

	// De rollen komen uit een tweede, even goedkope vraag: zonder de echte
	// rollijst zou de UI moeten gokken welke rollen deze site kent.
	rollen, err := s.leesRollen(ctx, tgt)
	if err == nil {
		rapport.Roles = rollen
	}
	return rapport, nil
}

// leesRollen asks WordPress for the roles registered on this install.
func (s *DBCloneService) leesRollen(ctx context.Context, tgt mediaTarget) ([]string, error) {
	out, err := s.ssh.RunCommand(ctx, tgt.SSH, buildRolesCommand(tgt.Webroot))
	if err != nil && !strings.Contains(out, "RDM-ROLE:") {
		return nil, err
	}
	return parseRoles(out), nil
}

// anonymiseerLokaal strips personal data from the just-imported local database.
//
// Dit gebeurt bewust ná de import en niet bij de export: de afspraak is
// "downloaden en daarna legen". Gevolg is dat de persoonsgegevens even op deze
// machine staan — in de dump en kort in de database. Daarom faalt deze stap hard
// in plaats van met een waarschuwing: een halve anonimisatie is erger dan een
// mislukte kloon, omdat je dan denkt dat het goed zit.
func (s *DBCloneService) anonymiseerLokaal(
	ctx context.Context, container, dbUser string, env []string, dbName string,
	cfg domain.AnonymiseCfg, prefix string,
) (domain.AnonymiseResult, error) {
	var res domain.AnonymiseResult

	inventaris, err := s.lokaleTabellen(ctx, container, dbUser, env, dbName)
	if err != nil {
		return res, fmt.Errorf("tabellen van de lokale database lezen: %w", err)
	}
	aanwezig := make(map[string]bool, len(inventaris))
	for _, t := range inventaris {
		aanwezig[t] = true
	}

	// 1. Gevoelige tabellen legen. Wat in de config staat maar niet in deze
	//    database zit, wordt gemeld in plaats van stil overgeslagen: dat is
	//    meestal een plugin die weg is, maar het kan ook een typefout zijn.
	var teLegen []string
	for _, t := range cfg.EmptyTables {
		if aanwezig[t] {
			teLegen = append(teLegen, t)
		} else {
			res.TablesMissing = append(res.TablesMissing, t)
		}
	}
	if len(teLegen) > 0 {
		sql, err := buildLocalEmptyTablesSQL(teLegen)
		if err != nil {
			return res, err
		}
		if err := s.voerLokaleSQL(ctx, container, dbUser, env, dbName, sql); err != nil {
			return res, fmt.Errorf("tabellen legen: %w", err)
		}
		res.TablesEmptied = teLegen
	}

	// 2. Gebruikers. De users-tabel is netwerkbreed, dus er is er precies één.
	if cfg.AnonymiseUsers {
		usersTabel := prefix + "users"
		usermetaTabel := prefix + "usermeta"
		if !aanwezig[usersTabel] || !aanwezig[usermetaTabel] {
			return res, fmt.Errorf("gebruikerstabellen %s/%s niet gevonden; anonimisatie afgebroken", usersTabel, usermetaTabel)
		}

		totaal, err := s.lokaleTelling(ctx, container, dbUser, env, dbName, "SELECT COUNT(*) FROM `"+usersTabel+"`;")
		if err != nil {
			return res, err
		}
		keptSQL, err := buildLocalKeptUserCountSQL(usersTabel, usermetaTabel, cfg.KeepRoles, cfg.KeepUserLogins)
		if err != nil {
			return res, err
		}
		bewaard, err := s.lokaleTelling(ctx, container, dbUser, env, dbName, keptSQL)
		if err != nil {
			return res, err
		}

		sql, err := buildLocalAnonymiseUsersSQL(usersTabel, usermetaTabel, cfg.KeepRoles, cfg.KeepUserLogins)
		if err != nil {
			return res, err
		}
		if err := s.voerLokaleSQL(ctx, container, dbUser, env, dbName, sql); err != nil {
			return res, fmt.Errorf("gebruikers anonimiseren: %w", err)
		}
		res.UsersKept = bewaard
		res.UsersAnonymised = totaal - bewaard

		if bewaard == 0 {
			res.Warnings = append(res.Warnings,
				"geen enkele gebruiker is bewaard — je kunt lokaal niet inloggen; kies bij Migratie › Instellingen een rol of account om te behouden")
		}
	}

	// 3. WooCommerce-orders in de oude opslag (zonder HPOS): die staan als posts
	//    in wp_posts met hun klantgegevens in wp_postmeta, waar geen TRUNCATE bij
	//    komt. Op een site zonder webshop raakt dit niets.
	if cfg.AnonymiseWooOrders {
		var paren [][2]string
		for _, t := range inventaris {
			if stripTabelPrefix(t, prefix) != "posts" {
				continue
			}
			postmeta := strings.TrimSuffix(t, "posts") + "postmeta"
			if aanwezig[postmeta] {
				paren = append(paren, [2]string{t, postmeta})
			}
		}
		if len(paren) > 0 {
			sql, err := buildLocalAnonymiseWooLegacyOrdersSQL(paren)
			if err != nil {
				return res, err
			}
			if err := s.voerLokaleSQL(ctx, container, dbUser, env, dbName, sql); err != nil {
				return res, fmt.Errorf("WooCommerce-orders anonimiseren: %w", err)
			}
			res.WooOrdersStripped = true
		}
	}

	// 4. Het e-mailadres van de beheerder in wp_options.
	if cfg.AnonymiseAdminEmail {
		var optionsTabellen []string
		for _, t := range inventaris {
			if stripTabelPrefix(t, prefix) == "options" {
				optionsTabellen = append(optionsTabellen, t)
			}
		}
		if len(optionsTabellen) > 0 {
			sql, err := buildLocalAnonymiseOptionsSQL(optionsTabellen)
			if err != nil {
				return res, err
			}
			if err := s.voerLokaleSQL(ctx, container, dbUser, env, dbName, sql); err != nil {
				return res, fmt.Errorf("beheerders-e-mailadres vervangen: %w", err)
			}
			res.AdminEmailReplaced = true
		}
	}

	// 5. Reacties, per site: op multisite bestaat er een comments-tabel per blog.
	if cfg.AnonymiseComments {
		var commentTabellen []string
		for _, t := range inventaris {
			if stripTabelPrefix(t, prefix) == "comments" {
				commentTabellen = append(commentTabellen, t)
			}
		}
		if len(commentTabellen) > 0 {
			aantal := int64(0)
			for _, t := range commentTabellen {
				n, err := s.lokaleTelling(ctx, container, dbUser, env, dbName, "SELECT COUNT(*) FROM `"+t+"`;")
				if err != nil {
					return res, err
				}
				aantal += n
			}
			sql, err := buildLocalAnonymiseCommentsSQL(commentTabellen)
			if err != nil {
				return res, err
			}
			if err := s.voerLokaleSQL(ctx, container, dbUser, env, dbName, sql); err != nil {
				return res, fmt.Errorf("reacties anonimiseren: %w", err)
			}
			res.CommentsAnonymised = aantal
		}
	}

	res.Limitations = anonymisatieBeperkingen()
	return res, nil
}

// anonymisatieBeperkingen zijn de dingen die deze anonimisatie niet dekt. Ze
// gaan mee in het resultaat in plaats van in documentatie: bij persoonsgegevens
// is een onvolledige belofte net zo riskant als een fout, en een lijst die de
// gebruiker ziet is de enige die wordt gelezen.
func anonymisatieBeperkingen() []string {
	return []string{
		"commentmeta: reactie-metadata van plugins (bv. beoordelingen) wordt niet doorzocht op persoonsgegevens",
		"berichtinhoud: persoonsgegevens die iemand in een pagina, bericht of custom field heeft getypt blijven staan",
		"plugin-opties: e-mailadressen in instellingen van plugins worden niet vervangen (behalve admin_email)",
		"onbekende plugintabellen: alleen tabellen uit de catalogus worden herkend — controleer de volledige lijst bij de inspectie",
	}
}

// lokaleTabellen lists the tables of the imported local database.
func (s *DBCloneService) lokaleTabellen(ctx context.Context, container, dbUser string, env []string, dbName string) ([]string, error) {
	var uit bytes.Buffer
	if err := s.dockerExec(ctx, container,
		[]string{"mysql", "-N", "-u" + dbUser, "-e", "SHOW TABLES", dbName}, env, nil, &uit); err != nil {
		return nil, err
	}
	var tabellen []string
	for _, r := range strings.Fields(strings.TrimSpace(uit.String())) {
		if r != "" {
			tabellen = append(tabellen, r)
		}
	}
	return tabellen, nil
}

// lokaleTelling runs a single COUNT query and returns the number.
func (s *DBCloneService) lokaleTelling(ctx context.Context, container, dbUser string, env []string, dbName, sql string) (int64, error) {
	var uit bytes.Buffer
	if err := s.dockerExec(ctx, container,
		[]string{"mysql", "-N", "-u" + dbUser, "-e", sql, dbName}, env, nil, &uit); err != nil {
		return 0, err
	}
	tekst := strings.TrimSpace(uit.String())
	n, err := strconv.ParseInt(tekst, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("onverwacht antwoord op een telling (%q)", tekst)
	}
	return n, nil
}

// voerLokaleSQL runs a multi-statement script against the local database.
func (s *DBCloneService) voerLokaleSQL(ctx context.Context, container, dbUser string, env []string, dbName, sql string) error {
	if strings.TrimSpace(sql) == "" {
		return nil
	}
	var stderr bytes.Buffer
	return s.dockerExec(ctx, container,
		[]string{"mysql", "-u" + dbUser, "-e", sql, dbName}, env, nil, &stderr)
}
