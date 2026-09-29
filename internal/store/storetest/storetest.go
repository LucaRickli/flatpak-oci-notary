// Package storetest opens test databases on every supported dialect: SQLite
// always, Postgres when NOTARY_TEST_POSTGRES_DSN is set (a URL such as
// postgres://notary:notary@localhost:55432/notary?sslmode=disable). Every
// target is isolated (a temp file, or a fresh schema dropped afterwards).
package storetest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/rs/zerolog"

	"github.com/lucarickli/flatpak-oci-notary/internal/store"
)

// PostgresEnv names the environment variable holding the Postgres test DSN.
const PostgresEnv = "NOTARY_TEST_POSTGRES_DSN"

// Target is a database to test against. Open it as often as needed to get
// several Store instances sharing one database.
type Target struct {
	Name   string
	Config store.Config
}

// Dialects names the available dialects.
func Dialects() []string {
	names := []string{store.DriverSQLite}
	if os.Getenv(PostgresEnv) != "" {
		names = append(names, store.DriverPostgres)
	}
	return names
}

// NewTarget creates an isolated database for the dialect, cleaned up when
// the test ends.
func NewTarget(t *testing.T, dialect string) Target {
	t.Helper()
	switch dialect {
	case store.DriverSQLite:
		return Target{Name: dialect, Config: store.Config{Driver: dialect, DSN: filepath.Join(t.TempDir(), "notary.db")}}
	case store.DriverPostgres:
		return Target{Name: dialect, Config: store.Config{Driver: dialect, DSN: freshSchema(t, os.Getenv(PostgresEnv))}}
	default:
		t.Fatalf("unknown dialect %q", dialect)
		return Target{}
	}
}

// Run runs fn once per dialect as a subtest, each on its own database.
func Run(t *testing.T, fn func(t *testing.T, tg Target)) {
	t.Helper()
	for _, dialect := range Dialects() {
		t.Run(dialect, func(t *testing.T) { fn(t, NewTarget(t, dialect)) })
	}
}

// Open opens a Store on the target; it is closed when the test ends. The
// generation cache is disabled so cross-instance tests are deterministic.
func (tg Target) Open(t *testing.T) *store.Store {
	t.Helper()
	cfg := tg.Config
	cfg.GenerationCacheTTL = -1
	cfg.Logger = zerolog.New(zerolog.NewTestWriter(t))
	s, err := store.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open %s store: %v", tg.Name, err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// SQLite opens a store on a fresh temporary SQLite database.
func SQLite(t *testing.T) *store.Store {
	t.Helper()
	return NewTarget(t, store.DriverSQLite).Open(t)
}

// freshSchema creates a schema for this test and returns a DSN using it.
func freshSchema(t *testing.T, dsn string) string {
	t.Helper()
	var b [6]byte
	_, _ = rand.Read(b[:])
	name := "t_" + hex.EncodeToString(b[:])
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if _, err := db.Exec(`CREATE SCHEMA "` + name + `"`); err != nil {
		db.Close()
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		defer db.Close()
		if _, err := db.Exec(`DROP SCHEMA "` + name + `" CASCADE`); err != nil {
			t.Errorf("drop schema %s: %v", name, err)
		}
	})
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "search_path=" + name
}
