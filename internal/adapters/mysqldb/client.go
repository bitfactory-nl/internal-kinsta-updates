package mysqldb

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
)

// Config beschrijft één lokale MySQL-verbinding.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
}

// lokaleHosts zijn de enige hosts waar deze editor mee mag verbinden. De feature
// is er voor de lokale docker-database; een productiehost hoort hier nooit in te
// komen, en dat wordt afgedwongen in plaats van aangenomen.
var lokaleHosts = map[string]bool{
	"127.0.0.1": true,
	"::1":       true,
	"localhost": true,
}

// IsLokaleHost reports whether host mag worden benaderd.
func IsLokaleHost(host string) bool {
	return lokaleHosts[strings.TrimSpace(strings.ToLower(host))]
}

// DSN bouwt de verbindingsstring.
func (c Config) DSN() (string, error) {
	if !IsLokaleHost(c.Host) {
		return "", fmt.Errorf("de database-editor verbindt alleen met een lokale database, niet met %q", c.Host)
	}
	if c.Port <= 0 || c.Port > 65535 {
		return "", fmt.Errorf("ongeldige poort %d", c.Port)
	}
	if c.User == "" {
		return "", fmt.Errorf("database-gebruiker ontbreekt")
	}

	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	cfg.User = c.User
	cfg.Passwd = c.Password
	cfg.DBName = c.DBName
	cfg.Collation = "utf8mb4_general_ci"
	// Bewust géén parseTime: een DATETIME komt dan terug precies zoals hij in de
	// kolom staat, zonder dat Go er een tijdzone bij verzint.
	cfg.ParseTime = false
	// Bewust géén interpolateParams: placeholders moeten door de server worden
	// afgehandeld, want dát is waarom er hier geen escaping-code staat.
	cfg.InterpolateParams = false
	cfg.MultiStatements = false
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 60 * time.Second
	cfg.WriteTimeout = 60 * time.Second
	return cfg.FormatDSN(), nil
}

// Client is een verbinding met één lokale MySQL-server.
type Client struct {
	db *sql.DB
}

// Open zet een verbindingspool op en controleert of de server bereikbaar is.
func Open(ctx context.Context, c Config) (*Client, error) {
	dsn, err := c.DSN()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("verbinding opzetten: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("geen verbinding met de database op %s:%d: %w", c.Host, c.Port, err)
	}
	return &Client{db: db}, nil
}

func (c *Client) Close() error { return c.db.Close() }

// DB geeft de onderliggende pool, voor transacties.
func (c *Client) DB() *sql.DB { return c.db }

// systeemSchemas blijven buiten de lijst: daar valt niets te bewerken.
var systeemSchemas = map[string]bool{
	"information_schema": true,
	"performance_schema": true,
	"mysql":              true,
	"sys":                true,
}

// Databases geeft de databases waar de gebruiker iets aan heeft.
func (c *Client) Databases(ctx context.Context) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, "SHOW DATABASES")
	if err != nil {
		return nil, fmt.Errorf("databases opvragen: %w", err)
	}
	defer rows.Close()

	var uit []string
	for rows.Next() {
		var naam string
		if err := rows.Scan(&naam); err != nil {
			return nil, err
		}
		if !systeemSchemas[naam] {
			uit = append(uit, naam)
		}
	}
	return uit, rows.Err()
}

// Tabel is één tabel met wat je nodig hebt om te beslissen of je erin wil kijken.
type Tabel struct {
	Naam string `json:"naam"`
	// Rijen is de schatting uit information_schema; voor InnoDB is die niet
	// exact, en dat staat er in de UI ook bij.
	Rijen       int64    `json:"rijen"`
	Bytes       int64    `json:"bytes"`
	Engine      string   `json:"engine"`
	Collation   string   `json:"collation"`
	PrimaryKeys []string `json:"primaryKeys"`
}

// Bewerkbaar geeft aan of rijen in deze tabel aangepast kunnen worden. Zonder
// primary key kan een UPDATE meer rijen raken dan bedoeld, dus dan niet.
func (t Tabel) Bewerkbaar() bool { return len(t.PrimaryKeys) > 0 }

// Tables geeft alle tabellen van een database, met hun primary keys.
func (c *Client) Tables(ctx context.Context, dbNaam string) ([]Tabel, error) {
	if err := ValideerIdent(dbNaam); err != nil {
		return nil, err
	}
	const q = `
		SELECT TABLE_NAME, IFNULL(TABLE_ROWS,0), IFNULL(DATA_LENGTH,0)+IFNULL(INDEX_LENGTH,0),
		       IFNULL(ENGINE,''), IFNULL(TABLE_COLLATION,'')
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'
		ORDER BY TABLE_NAME`
	rows, err := c.db.QueryContext(ctx, q, dbNaam)
	if err != nil {
		return nil, fmt.Errorf("tabellen opvragen: %w", err)
	}
	defer rows.Close()

	tabellen := make([]Tabel, 0)
	for rows.Next() {
		var t Tabel
		if err := rows.Scan(&t.Naam, &t.Rijen, &t.Bytes, &t.Engine, &t.Collation); err != nil {
			return nil, err
		}
		tabellen = append(tabellen, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	pks, err := c.primaryKeys(ctx, dbNaam)
	if err != nil {
		return nil, err
	}
	for i := range tabellen {
		tabellen[i].PrimaryKeys = pks[tabellen[i].Naam]
	}
	return tabellen, nil
}

// primaryKeys geeft per tabel de kolommen van de primary key, in de juiste orde.
func (c *Client) primaryKeys(ctx context.Context, dbNaam string) (map[string][]string, error) {
	const q = `
		SELECT TABLE_NAME, COLUMN_NAME
		FROM information_schema.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = ? AND CONSTRAINT_NAME = 'PRIMARY'
		ORDER BY TABLE_NAME, ORDINAL_POSITION`
	rows, err := c.db.QueryContext(ctx, q, dbNaam)
	if err != nil {
		return nil, fmt.Errorf("primary keys opvragen: %w", err)
	}
	defer rows.Close()

	uit := map[string][]string{}
	for rows.Next() {
		var tabel, kolom string
		if err := rows.Scan(&tabel, &kolom); err != nil {
			return nil, err
		}
		uit[tabel] = append(uit[tabel], kolom)
	}
	return uit, rows.Err()
}

// Kolom beschrijft één kolom.
type Kolom struct {
	Naam       string `json:"naam"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	IsPK       bool   `json:"isPk"`
	Standaard  string `json:"standaard"`
	AutoIncr   bool   `json:"autoIncr"`
	Commentaar string `json:"commentaar"`
}

// Columns geeft de kolommen van een tabel in kolomvolgorde.
func (c *Client) Columns(ctx context.Context, dbNaam, tabel string) ([]Kolom, error) {
	if err := ValideerIdent(dbNaam); err != nil {
		return nil, err
	}
	if err := ValideerIdent(tabel); err != nil {
		return nil, err
	}
	const q = `
		SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY,
		       IFNULL(COLUMN_DEFAULT,''), EXTRA, IFNULL(COLUMN_COMMENT,'')
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`
	rows, err := c.db.QueryContext(ctx, q, dbNaam, tabel)
	if err != nil {
		return nil, fmt.Errorf("kolommen opvragen: %w", err)
	}
	defer rows.Close()

	kolommen := make([]Kolom, 0)
	for rows.Next() {
		var k Kolom
		var nullable, key, extra string
		if err := rows.Scan(&k.Naam, &k.Type, &nullable, &key, &k.Standaard, &extra, &k.Commentaar); err != nil {
			return nil, err
		}
		k.Nullable = nullable == "YES"
		k.IsPK = key == "PRI"
		k.AutoIncr = strings.Contains(extra, "auto_increment")
		kolommen = append(kolommen, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(kolommen) == 0 {
		return nil, fmt.Errorf("tabel %q bestaat niet in %q", tabel, dbNaam)
	}
	return kolommen, nil
}

// Cel is één waarde uit een resultaat. Null is apart van een lege string, want in
// een database is dat een wezenlijk verschil dat je in een editor moet zien.
type Cel struct {
	Waarde   string `json:"waarde"`
	Null     bool   `json:"null"`
	Binair   bool   `json:"binair"`
	Bytes    int    `json:"bytes"`
	Afgekapt bool   `json:"afgekapt"`
}

// maxCelTekens begrenst wat er per cel naar de frontend gaat; een longtext met
// een halve megabyte aan HTML maakt de tabel onbruikbaar.
const maxCelTekens = 2000

func celVan(ruw any) Cel {
	if ruw == nil {
		return Cel{Null: true}
	}
	var b []byte
	switch v := ruw.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return Cel{Waarde: fmt.Sprint(v)}
	}
	if !utf8.Valid(b) {
		return Cel{Binair: true, Bytes: len(b), Waarde: fmt.Sprintf("(binair, %d bytes)", len(b))}
	}
	s := string(b)
	if len(s) > maxCelTekens {
		return Cel{Waarde: s[:veiligeKnip(s, maxCelTekens)], Bytes: len(b), Afgekapt: true}
	}
	return Cel{Waarde: s, Bytes: len(b)}
}

func veiligeKnip(s string, n int) int {
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return n
}

// Resultaat is een resultaatset.
type Resultaat struct {
	Kolommen []string `json:"kolommen"`
	Rijen    [][]Cel  `json:"rijen"`
	// Afgekapt is true als er meer rijen waren dan het maximum.
	Afgekapt bool `json:"afgekapt"`
	// Duur is hoe lang de query deed, voor de statusregel.
	DuurMs int64 `json:"duurMs"`
}

// scanResultaat leest een sql.Rows uit tot maxRijen.
func scanResultaat(rows *sql.Rows, maxRijen int) (Resultaat, error) {
	kolommen, err := rows.Columns()
	if err != nil {
		return Resultaat{}, err
	}
	uit := Resultaat{Kolommen: kolommen, Rijen: make([][]Cel, 0)}

	for rows.Next() {
		if len(uit.Rijen) >= maxRijen {
			uit.Afgekapt = true
			break
		}
		ruw := make([]any, len(kolommen))
		verwijzingen := make([]any, len(kolommen))
		for i := range ruw {
			verwijzingen[i] = &ruw[i]
		}
		if err := rows.Scan(verwijzingen...); err != nil {
			return uit, err
		}
		rij := make([]Cel, len(kolommen))
		for i, w := range ruw {
			rij[i] = celVan(w)
		}
		uit.Rijen = append(uit.Rijen, rij)
	}
	return uit, rows.Err()
}
