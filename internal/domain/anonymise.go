package domain

// AVG-categorieën waarin gevonden tabellen worden ingedeeld. De namen komen
// terug in de UI, dus ze zijn Nederlands en beschrijven wat er in zit.
const (
	AVGFormulieren = "formulieren"
	AVGNieuwsbrief = "nieuwsbrief"
	AVGWebshop     = "webshop"
	AVGLogboek     = "logboek"
	AVGOverig      = "overig"
)

// SensitiveTable is one table found on the production database that holds
// personal data, with the reason it was flagged so the user can judge it
// instead of trusting a checkbox.
type SensitiveTable struct {
	Name     string `json:"name"`     // volledige tabelnaam, inclusief prefix
	Category string `json:"category"` // een van de AVG-constanten hierboven
	Reason   string `json:"reason"`   // wat er in deze tabel staat
	Rows     int64  `json:"rows"`     // benadering uit information_schema
}

// AnonymiseCfg lives under `migration.anonymise:` in .rdm.yml and says what a
// clone must strip to stay within the AVG. It holds no personal data itself —
// only role names, logins and table names — so it is safe in the customer's
// repo.
type AnonymiseCfg struct {
	// Enabled uit betekent: klonen zonder te anonimiseren. Dat is een bewuste
	// keuze die zichtbaar in het resultaat wordt gemeld, nooit een stille default.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// KeepRoles/KeepUserLogins zijn de accounts die ongemoeid blijven — meestal
	// de eigen beheerders, zodat je kunt inloggen. Al het andere wordt
	// geanonimiseerd.
	KeepRoles      []string `yaml:"keep_roles,omitempty"       json:"keepRoles,omitempty"`
	KeepUserLogins []string `yaml:"keep_user_logins,omitempty" json:"keepUserLogins,omitempty"`

	AnonymiseUsers    bool `yaml:"anonymise_users"    json:"anonymiseUsers"`
	AnonymiseComments bool `yaml:"anonymise_comments" json:"anonymiseComments"`

	// EmptyTables zijn de tabellen die na de import worden geleegd. Het schema
	// blijft staan, zodat plugins niet omvallen over een ontbrekende tabel.
	EmptyTables []string `yaml:"empty_tables,omitempty" json:"emptyTables,omitempty"`
}

// AnonymiseResult reports what the anonymisation actually did, so it can be
// shown after a clone instead of being taken on faith.
type AnonymiseResult struct {
	Skipped            bool     `json:"skipped"` // anonimisatie stond uit
	TablesEmptied      []string `json:"tablesEmptied,omitempty"`
	TablesMissing      []string `json:"tablesMissing,omitempty"` // stonden in de config maar niet in de DB
	UsersAnonymised    int64    `json:"usersAnonymised"`
	UsersKept          int64    `json:"usersKept"`
	CommentsAnonymised int64    `json:"commentsAnonymised"`
	Warnings           []string `json:"warnings,omitempty"`
}
