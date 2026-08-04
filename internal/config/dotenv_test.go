package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const dotenvFixture = `DB_NAME=dev_vanluykennl
DB_USER=root
DB_PASSWORD=secret
DB_HOST=mysql

# lokale overrides
APP_DOMAIN=vanluykennl.test
`

func TestParseDotenv(t *testing.T) {
	want := map[string]string{
		"DB_NAME":     "dev_vanluykennl",
		"DB_USER":     "root",
		"DB_PASSWORD": "secret",
		"DB_HOST":     "mysql",
		"APP_DOMAIN":  "vanluykennl.test",
	}
	got := ParseDotenv([]byte(dotenvFixture))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseDotenv() = %+v, want %+v", got, want)
	}
}

func TestParseDotenvIgnoresBlankAndComments(t *testing.T) {
	data := []byte("\n# eerste comment\n  # ingesprongen comment\nKEY=value\n\n# laatste\n")
	got := ParseDotenv(data)
	want := map[string]string{"KEY": "value"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseDotenv() = %+v, want %+v", got, want)
	}
}

func TestParseDotenvIgnoresLinesWithoutEquals(t *testing.T) {
	data := []byte("DB_NAME=dev_vanluykennl\nnietsaanhetdoen\nDB_USER=root\n")
	got := ParseDotenv(data)
	want := map[string]string{
		"DB_NAME": "dev_vanluykennl",
		"DB_USER": "root",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseDotenv() = %+v, want %+v", got, want)
	}
}

func TestLoadProjectEnvMissingFile(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadProjectEnv(dir)
	if err != nil {
		t.Fatalf("LoadProjectEnv: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map for missing .env, got %+v", got)
	}
}

func TestLoadProjectEnvExisting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(dotenvFixture), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := LoadProjectEnv(dir)
	if err != nil {
		t.Fatalf("LoadProjectEnv: %v", err)
	}
	want := map[string]string{
		"DB_NAME":     "dev_vanluykennl",
		"DB_USER":     "root",
		"DB_PASSWORD": "secret",
		"DB_HOST":     "mysql",
		"APP_DOMAIN":  "vanluykennl.test",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadProjectEnv() = %+v, want %+v", got, want)
	}
}
