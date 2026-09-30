package domain

// LocalProject is een lokale map zonder WordPress-koppeling (Laravel, Pimcore,
// overig) die de gebruiker zelf aan de tool heeft toegevoegd.
type LocalProject struct {
	Path string `yaml:"path" json:"path"`
	Name string `yaml:"name,omitempty" json:"name"`
}
