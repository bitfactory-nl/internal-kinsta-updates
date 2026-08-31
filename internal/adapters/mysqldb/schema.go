package mysqldb

import (
	"context"
	"fmt"
	"strings"
)

// SchemaTekst beschrijft een database compact: één regel per tabel met de
// kolommen en hun types.
//
// Dit is wat er naar een AI gaat om een vraag in SQL om te zetten. Er zit
// bewust géén rijdata in — alleen namen en types. Een WordPress-database met 70
// tabellen komt hiermee op ongeveer 10 KB, dus het hele schema kan mee en er
// hoeft niet gegokt te worden welke tabellen relevant zijn.
//
// Naast de tekst komt de lijst tabelnamen terug, zodat de aanroeper kan zien
// welke bekende tabellen een gegenereerde query noemt.
func (c *Client) SchemaTekst(ctx context.Context, dbNaam string) (string, []string, error) {
	if err := ValideerIdent(dbNaam); err != nil {
		return "", nil, err
	}
	const q = `
		SELECT TABLE_NAME, COLUMN_NAME, COLUMN_TYPE, COLUMN_KEY
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ?
		ORDER BY TABLE_NAME, ORDINAL_POSITION`
	rows, err := c.db.QueryContext(ctx, q, dbNaam)
	if err != nil {
		return "", nil, fmt.Errorf("schema opvragen: %w", err)
	}
	defer rows.Close()

	var (
		uit      strings.Builder
		tabellen []string
		huidig   string
		eerste   bool
	)
	for rows.Next() {
		var tabel, kolom, type_, key string
		if err := rows.Scan(&tabel, &kolom, &type_, &key); err != nil {
			return "", nil, err
		}
		if tabel != huidig {
			if huidig != "" {
				uit.WriteString(")\n")
			}
			uit.WriteString(tabel)
			uit.WriteString("(")
			huidig = tabel
			tabellen = append(tabellen, tabel)
			eerste = true
		}
		if !eerste {
			uit.WriteString(", ")
		}
		eerste = false
		uit.WriteString(kolom)
		uit.WriteString(" ")
		uit.WriteString(type_)
		if key == "PRI" {
			uit.WriteString(" PK")
		}
	}
	if err := rows.Err(); err != nil {
		return "", nil, err
	}
	if huidig != "" {
		uit.WriteString(")\n")
	}
	if len(tabellen) == 0 {
		return "", nil, fmt.Errorf("database %q heeft geen tabellen", dbNaam)
	}
	return uit.String(), tabellen, nil
}
