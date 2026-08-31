package mysqldb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// MaxRijenPerPagina is de bovengrens van wat één pagina mag opleveren.
const MaxRijenPerPagina = 500

// MaxRijenQuery is de bovengrens voor een handmatige query. Zonder grens kan één
// SELECT zonder LIMIT de hele app laten hangen op een tabel met miljoenen rijen.
const MaxRijenQuery = 2000

// RijenOpties beschrijft welke pagina van welke tabel je wil.
type RijenOpties struct {
	Database string
	Tabel    string
	// Sorteer is een kolomnaam; leeg betekent de natuurlijke volgorde.
	Sorteer  string
	Aflopend bool
	// ZoekKolom en Zoek filteren met LIKE. De waarde gaat als placeholder mee.
	ZoekKolom string
	Zoek      string
	Limiet    int
	Offset    int
}

// bestaandeKolommen geeft de kolomnamen van een tabel als set.
func (c *Client) bestaandeKolommen(ctx context.Context, dbNaam, tabel string) (map[string]bool, error) {
	kolommen, err := c.Columns(ctx, dbNaam, tabel)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(kolommen))
	for _, k := range kolommen {
		set[k.Naam] = true
	}
	return set, nil
}

// eisKolom controleert dat een kolomnaam echt in deze tabel bestaat.
//
// Dit is de tweede helft van de identifier-afscherming: ValideerIdent zegt dat de
// naam onschuldig ís, deze functie zegt dat hij ook echt bestaat. Zo kan een naam
// die de frontend verzint nooit in een query terechtkomen.
func eisKolom(set map[string]bool, naam string) error {
	if err := ValideerIdent(naam); err != nil {
		return err
	}
	if !set[naam] {
		return fmt.Errorf("kolom %q bestaat niet in deze tabel", naam)
	}
	return nil
}

// Rijen haalt één pagina op.
func (c *Client) Rijen(ctx context.Context, o RijenOpties) (Resultaat, error) {
	kolomSet, err := c.bestaandeKolommen(ctx, o.Database, o.Tabel)
	if err != nil {
		return Resultaat{}, err
	}
	volledig, err := QuoteIdentPad(o.Database, o.Tabel)
	if err != nil {
		return Resultaat{}, err
	}

	var sb strings.Builder
	var args []any
	sb.WriteString("SELECT * FROM " + volledig)

	if o.Zoek != "" && o.ZoekKolom != "" {
		if err := eisKolom(kolomSet, o.ZoekKolom); err != nil {
			return Resultaat{}, err
		}
		q, _ := QuoteIdent(o.ZoekKolom)
		sb.WriteString(" WHERE " + q + " LIKE ?")
		args = append(args, "%"+o.Zoek+"%")
	}
	if o.Sorteer != "" {
		if err := eisKolom(kolomSet, o.Sorteer); err != nil {
			return Resultaat{}, err
		}
		q, _ := QuoteIdent(o.Sorteer)
		sb.WriteString(" ORDER BY " + q)
		if o.Aflopend {
			sb.WriteString(" DESC")
		} else {
			sb.WriteString(" ASC")
		}
	}

	limiet := o.Limiet
	if limiet <= 0 || limiet > MaxRijenPerPagina {
		limiet = MaxRijenPerPagina
	}
	offset := o.Offset
	if offset < 0 {
		offset = 0
	}
	// Eén extra rij opvragen dan we tonen: zo weten we of er nog meer is zonder
	// een tweede query.
	sb.WriteString(" LIMIT ? OFFSET ?")
	args = append(args, limiet+1, offset)

	start := time.Now()
	rows, err := c.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return Resultaat{}, fmt.Errorf("rijen opvragen: %w", err)
	}
	defer rows.Close()

	res, err := scanResultaat(rows, limiet+1)
	if err != nil {
		return Resultaat{}, err
	}
	if len(res.Rijen) > limiet {
		res.Rijen = res.Rijen[:limiet]
		res.Afgekapt = true
	}
	res.DuurMs = time.Since(start).Milliseconds()
	return res, nil
}

// Aantal telt de rijen die bij dezelfde filter horen.
func (c *Client) Aantal(ctx context.Context, o RijenOpties) (int64, error) {
	kolomSet, err := c.bestaandeKolommen(ctx, o.Database, o.Tabel)
	if err != nil {
		return 0, err
	}
	volledig, err := QuoteIdentPad(o.Database, o.Tabel)
	if err != nil {
		return 0, err
	}

	q := "SELECT COUNT(*) FROM " + volledig
	var args []any
	if o.Zoek != "" && o.ZoekKolom != "" {
		if err := eisKolom(kolomSet, o.ZoekKolom); err != nil {
			return 0, err
		}
		kq, _ := QuoteIdent(o.ZoekKolom)
		q += " WHERE " + kq + " LIKE ?"
		args = append(args, "%"+o.Zoek+"%")
	}

	var n int64
	if err := c.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("rijen tellen: %w", err)
	}
	return n, nil
}

// Select voert een lees-query uit zoals de gebruiker die typte.
func (c *Client) Select(ctx context.Context, sqlTekst string) (Resultaat, error) {
	start := time.Now()
	rows, err := c.db.QueryContext(ctx, sqlTekst)
	if err != nil {
		return Resultaat{}, err
	}
	defer rows.Close()

	res, err := scanResultaat(rows, MaxRijenQuery)
	if err != nil {
		return Resultaat{}, err
	}
	res.DuurMs = time.Since(start).Milliseconds()
	return res, nil
}

// Exec voert een query uit die geen rijen teruggeeft en meldt hoeveel rijen
// geraakt zijn.
func (c *Client) Exec(ctx context.Context, sqlTekst string) (int64, int64, error) {
	start := time.Now()
	res, err := c.db.ExecContext(ctx, sqlTekst)
	if err != nil {
		return 0, 0, err
	}
	geraakt, _ := res.RowsAffected()
	return geraakt, time.Since(start).Milliseconds(), nil
}

// SleutelWaarde is één primary-key-kolom met zijn waarde.
type SleutelWaarde struct {
	Kolom  string `json:"kolom"`
	Waarde string `json:"waarde"`
	Null   bool   `json:"null"`
}

// bouwSleutelWhere maakt de WHERE voor precies één rij.
func bouwSleutelWhere(kolomSet map[string]bool, sleutel []SleutelWaarde) (string, []any, error) {
	if len(sleutel) == 0 {
		return "", nil, fmt.Errorf("zonder primary key kan er geen enkele rij veilig aangewezen worden")
	}
	var delen []string
	var args []any
	for _, s := range sleutel {
		if err := eisKolom(kolomSet, s.Kolom); err != nil {
			return "", nil, err
		}
		q, _ := QuoteIdent(s.Kolom)
		if s.Null {
			delen = append(delen, q+" IS NULL")
			continue
		}
		delen = append(delen, q+" = ?")
		args = append(args, s.Waarde)
	}
	return strings.Join(delen, " AND "), args, nil
}

// eisPreciesEenRij controleert binnen de transactie dat de WHERE één rij raakt.
//
// Dit is de vangrail onder het bewerken: een samengestelde sleutel die niet
// volledig is meegegeven, of een waarde die door type-conversie op meer rijen
// past, zou anders stil meerdere rijen aanpassen.
func eisPreciesEenRij(ctx context.Context, tx *sql.Tx, volledig, where string, args []any) error {
	var n int64
	q := "SELECT COUNT(*) FROM " + volledig + " WHERE " + where
	if err := tx.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return fmt.Errorf("rij opzoeken: %w", err)
	}
	switch {
	case n == 0:
		return fmt.Errorf("die rij bestaat niet meer; verversen en opnieuw proberen")
	case n > 1:
		return fmt.Errorf("deze sleutel past op %d rijen, dus ik wijzig er geen enkele", n)
	}
	return nil
}

// UpdateCel zet één cel op een nieuwe waarde. nieuw is nil voor NULL.
func (c *Client) UpdateCel(ctx context.Context, dbNaam, tabel string, sleutel []SleutelWaarde, kolom string, nieuw *string) error {
	kolomSet, err := c.bestaandeKolommen(ctx, dbNaam, tabel)
	if err != nil {
		return err
	}
	if err := eisKolom(kolomSet, kolom); err != nil {
		return err
	}
	volledig, err := QuoteIdentPad(dbNaam, tabel)
	if err != nil {
		return err
	}
	where, whereArgs, err := bouwSleutelWhere(kolomSet, sleutel)
	if err != nil {
		return err
	}
	kq, _ := QuoteIdent(kolom)

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("transactie starten: %w", err)
	}
	defer tx.Rollback()

	if err := eisPreciesEenRij(ctx, tx, volledig, where, whereArgs); err != nil {
		return err
	}

	args := make([]any, 0, len(whereArgs)+1)
	if nieuw == nil {
		args = append(args, nil)
	} else {
		args = append(args, *nieuw)
	}
	args = append(args, whereArgs...)

	if _, err := tx.ExecContext(ctx, "UPDATE "+volledig+" SET "+kq+" = ? WHERE "+where, args...); err != nil {
		return err
	}
	return tx.Commit()
}

// VerwijderRij verwijdert precies één rij.
func (c *Client) VerwijderRij(ctx context.Context, dbNaam, tabel string, sleutel []SleutelWaarde) error {
	kolomSet, err := c.bestaandeKolommen(ctx, dbNaam, tabel)
	if err != nil {
		return err
	}
	volledig, err := QuoteIdentPad(dbNaam, tabel)
	if err != nil {
		return err
	}
	where, args, err := bouwSleutelWhere(kolomSet, sleutel)
	if err != nil {
		return err
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("transactie starten: %w", err)
	}
	defer tx.Rollback()

	if err := eisPreciesEenRij(ctx, tx, volledig, where, args); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+volledig+" WHERE "+where, args...); err != nil {
		return err
	}
	return tx.Commit()
}

// NieuweWaarde is één kolom met de in te voegen waarde.
type NieuweWaarde struct {
	Kolom  string `json:"kolom"`
	Waarde string `json:"waarde"`
	Null   bool   `json:"null"`
}

// VoegRijToe voegt een rij toe en geeft het auto-increment-id terug als er een is.
func (c *Client) VoegRijToe(ctx context.Context, dbNaam, tabel string, waarden []NieuweWaarde) (int64, error) {
	kolomSet, err := c.bestaandeKolommen(ctx, dbNaam, tabel)
	if err != nil {
		return 0, err
	}
	volledig, err := QuoteIdentPad(dbNaam, tabel)
	if err != nil {
		return 0, err
	}
	if len(waarden) == 0 {
		// Zonder kolommen is een lege rij nog steeds geldig als alle kolommen een
		// standaardwaarde hebben.
		res, err := c.db.ExecContext(ctx, "INSERT INTO "+volledig+" () VALUES ()")
		if err != nil {
			return 0, err
		}
		id, _ := res.LastInsertId()
		return id, nil
	}

	var kolommen []string
	var plaatsen []string
	var args []any
	for _, w := range waarden {
		if err := eisKolom(kolomSet, w.Kolom); err != nil {
			return 0, err
		}
		q, _ := QuoteIdent(w.Kolom)
		kolommen = append(kolommen, q)
		plaatsen = append(plaatsen, "?")
		if w.Null {
			args = append(args, nil)
		} else {
			args = append(args, w.Waarde)
		}
	}

	q := "INSERT INTO " + volledig + " (" + strings.Join(kolommen, ", ") + ") VALUES (" + strings.Join(plaatsen, ", ") + ")"
	res, err := c.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}
