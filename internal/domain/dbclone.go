package domain

// DBProbe is a quick, read-only check of the production environment: what is
// there and how big it is.
type DBProbe struct {
	SiteURL     string `json:"siteUrl"`
	TablePrefix string `json:"tablePrefix"`
	IsMultisite bool   `json:"isMultisite"`
	// NetworkDomain is the production network's actual registered domain
	// (from wp_site, via `global $current_site`) — only meaningful when
	// IsMultisite. This is the bare-hostname search term for the multisite
	// domain fix; it is not always the same as the bare host of SiteURL.
	NetworkDomain string `json:"networkDomain,omitempty"`
	DBSizeBytes   int64  `json:"dbSizeBytes"`
	TmpFreeBytes  int64  `json:"tmpFreeBytes,omitempty"`
}

// LocalEnvDefaults are the local defaults read from the project's own .env
// file — purely local, no SSH needed — used to prefill form fields. The
// multisite fields come straight from the project's own .env conventions
// (MULTISITE, SUBDOMAIN_INSTALL, DOMAIN_CURRENT_SITE), which is the clearest,
// most authoritative signal of whether — and how — a project runs as
// multisite locally; it is not inferred or guessed.
type LocalEnvDefaults struct {
	DBName string `json:"dbName"`
	DBHost string `json:"dbHost"` // "mysql" | "mysql84" | empty if unknown
	URL    string `json:"url"`    // https://<APP_DOMAIN>, empty if .env is absent

	IsMultisite       bool   `json:"isMultisite"`
	SubdomainInstall  bool   `json:"subdomainInstall"`
	DomainCurrentSite string `json:"domainCurrentSite,omitempty"` // from .env DOMAIN_CURRENT_SITE
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
	// ProdNetworkDomain/LocalNetworkDomain are the bare hostnames the
	// multisite fix rewrites wp_blogs.domain/wp_site.domain from and to. Both
	// are optional; when empty, Clone falls back to the bare host of
	// ProdSiteURL/LocalURL. Prefer these when known (ProdNetworkDomain from a
	// Probe, LocalNetworkDomain from .env's DOMAIN_CURRENT_SITE) since a
	// subdomain-multisite network's domain is not always the same as its
	// primary site's URL.
	ProdNetworkDomain  string `json:"prodNetworkDomain,omitempty"`
	LocalNetworkDomain string `json:"localNetworkDomain,omitempty"`
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
