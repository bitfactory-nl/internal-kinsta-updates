// Package version houdt bij welke versie van de app draait. Version wordt bij
// het bouwen gestempeld met de git-tag; blijft dat achterwege, dan is dit een
// lokale build en staat zelf-update uit.
package version

// Version is de versie van deze build, gezet via
// -ldflags "-X github.com/rdm/sites-tool/internal/version.Version=v0.3.0".
var Version = "dev"

// IsDev meldt of dit een build zonder versiestempel is. Zulke builds bieden
// geen updates aan: ze zouden zichzelf vervangen door een release waarvan niet
// vast te stellen is of die nieuwer is dan de code die nu draait.
func IsDev() bool {
	return Version == "" || Version == "dev"
}
