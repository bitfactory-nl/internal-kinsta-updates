package domain

// DBProbe is a quick, read-only check of the production environment: what is
// there and how big it is.
type DBProbe struct {
	SiteURL      string `json:"siteUrl"`
	TablePrefix  string `json:"tablePrefix"`
	IsMultisite  bool   `json:"isMultisite"`
	DBSizeBytes  int64  `json:"dbSizeBytes"`
	TmpFreeBytes int64  `json:"tmpFreeBytes,omitempty"`
}

// LocalEnvDefaults are the local defaults read from the project's own .env
// file — purely local, no SSH needed — used to prefill form fields.
type LocalEnvDefaults struct {
	DBName string `json:"dbName"`
	DBHost string `json:"dbHost"` // "mysql" | "mysql84" | empty if unknown
	URL    string `json:"url"`    // https://<APP_DOMAIN>, empty if .env is absent
}

// DBCloneRequest is what the user has filled in/confirmed in the UI for one
// clone operation.
type DBCloneRequest struct {
	ProdSiteURL string `json:"prodSiteUrl"`
	LocalURL    string `json:"localUrl"`
	LocalDBName string `json:"localDbName"`
	LocalDBHost string `json:"localDbHost"` // "mysql" | "mysql84"
	// TablePrefix comes from an earlier Probe, so the service does not need to
	// probe again just for the multisite domain fix.
	TablePrefix string `json:"tablePrefix"`
	Multisite   bool   `json:"multisite"`
}

// DBCloneProgress is one step of a running clone, sent as a Wails event.
type DBCloneProgress struct {
	Phase  string `json:"phase"` // "backup" | "export" | "download" | "import" | "multisite-fix" | "verify" | "done" | "error"
	Detail string `json:"detail"`
	Bytes  int64  `json:"bytes,omitempty"`
	Total  int64  `json:"total,omitempty"`
}

// DBCloneResult is the outcome of a completed clone.
type DBCloneResult struct {
	LocalDBName         string   `json:"localDbName"`
	SiteURLBefore       string   `json:"siteUrlBefore"`
	SiteURLAfter        string   `json:"siteUrlAfter"`
	TablesImported      int      `json:"tablesImported"`
	DumpBytes           int64    `json:"dumpBytes"`
	BackupPath          string   `json:"backupPath,omitempty"`
	MultisiteFixApplied bool     `json:"multisiteFixApplied"`
	Warnings            []string `json:"warnings,omitempty"`
}
