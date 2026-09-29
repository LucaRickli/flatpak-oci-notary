package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/lucarickli/flatpak-oci-notary/internal/indexer"
	"github.com/lucarickli/flatpak-oci-notary/internal/store"
)

// flagSpec describes a flag bound to a viper key. The key is what the
// config file and the NOTARY_* environment variables use; the flag is its
// command-line form.
type flagSpec struct {
	key, flag string
	def       any
	usage     string
}

// Flag groups shared by the commands that open the database and sync.
var (
	databaseFlags = []flagSpec{
		{"data-dir", "data-dir", "data", "directory for the SQLite database"},
		{"database.driver", "database-driver", store.DriverSQLite, "database driver: sqlite or postgres"},
		{"database.dsn", "database-dsn", "", "postgres connection URL, or the sqlite file path (default: <data-dir>/notary.db)"},
		{"database.max-open-conns", "database-max-open-conns", 10, "maximum open connections (postgres; 0 = unlimited)"},
		{"database.max-idle-conns", "database-max-idle-conns", 5, "maximum idle connections (postgres)"},
		{"database.conn-max-lifetime", "database-conn-max-lifetime", 30 * time.Minute, "maximum connection lifetime (postgres; 0 = unlimited)"},
	}
	syncFlags = []flagSpec{
		{"sync.concurrency", "sync-concurrency", 4, "OCI repositories indexed in parallel per registry"},
		{"sync.timeout", "sync-timeout", 30 * time.Minute, "maximum duration of a registry sync"},
		{"sync.lease", "sync-lease", 2 * time.Minute, "sync lease duration; a registry left syncing by a dead process is retried after this long"},
		{"sync.instance", "sync-instance", "", "identity of this process in sync leases, unique among the processes sharing the database (default: <hostname>:<listen port> for serve, <hostname>:sync-<pid>-<random> for sync)"},
	}
	// checkIntervalFlag is used by the scheduler: serve and sync --watch.
	checkIntervalFlag = flagSpec{"sync.check-interval", "sync-check-interval", 30 * time.Second, "how often to check for registries due for a sync"}
)

// defineFlags adds the flags to f.
func defineFlags(f *pflag.FlagSet, specs ...[]flagSpec) {
	for _, group := range specs {
		for _, fl := range group {
			switch d := fl.def.(type) {
			case string:
				f.String(fl.flag, d, fl.usage)
			case bool:
				f.Bool(fl.flag, d, fl.usage)
			case int:
				f.Int(fl.flag, d, fl.usage)
			case time.Duration:
				f.Duration(fl.flag, d, fl.usage)
			case []string:
				f.StringSlice(fl.flag, d, fl.usage)
			default:
				panic(fmt.Sprintf("flag %s: unsupported default %T", fl.flag, fl.def))
			}
		}
	}
}

// bindFlags binds the flags of the running command to their viper keys. It
// is called when the command runs, not when it is defined: serve and sync
// share keys, and viper keeps one flag per key, so binding both commands'
// flags up front would make one command read the other's (unset) flags.
func bindFlags(v *viper.Viper, f *pflag.FlagSet, specs ...[]flagSpec) {
	for _, group := range specs {
		for _, fl := range group {
			mustBind(v, fl.key, f.Lookup(fl.flag))
		}
	}
}

func mustBind(v *viper.Viper, key string, flag *pflag.Flag) {
	if flag == nil {
		panic("flag for " + key + " not defined")
	}
	if err := v.BindPFlag(key, flag); err != nil {
		panic(err)
	}
}

// databaseConfig reads the database settings. The DSN may carry a
// password, so it is never logged as-is.
func databaseConfig(v *viper.Viper, log zerolog.Logger) store.Config {
	cfg := store.Config{
		Driver:          strings.ToLower(strings.TrimSpace(v.GetString("database.driver"))),
		DSN:             strings.TrimSpace(v.GetString("database.dsn")),
		MaxOpenConns:    v.GetInt("database.max-open-conns"),
		MaxIdleConns:    v.GetInt("database.max-idle-conns"),
		ConnMaxLifetime: v.GetDuration("database.conn-max-lifetime"),
		Logger:          log,
	}
	if cfg.Driver == store.DriverSQLite && cfg.DSN == "" {
		cfg.DSN = filepath.Join(v.GetString("data-dir"), "notary.db")
	}
	return cfg
}

// openStore opens the configured database.
func openStore(ctx context.Context, v *viper.Viper, log zerolog.Logger) (*store.Store, store.Config, error) {
	cfg := databaseConfig(v, log)
	st, err := store.Open(ctx, cfg)
	if err != nil {
		return nil, cfg, fmt.Errorf("open database: %w", err)
	}
	return st, cfg, nil
}

// newIndexer builds the indexer from the sync settings, with the given
// instance identity.
func newIndexer(v *viper.Viper, st *store.Store, log zerolog.Logger, owner string) *indexer.Indexer {
	return indexer.New(st, log, indexer.Options{
		Concurrency:   v.GetInt("sync.concurrency"),
		SyncTimeout:   v.GetDuration("sync.timeout"),
		CheckInterval: v.GetDuration("sync.check-interval"),
		LeaseDuration: v.GetDuration("sync.lease"),
		Owner:         owner,
	})
}

// instanceID returns the configured instance identity, or the hostname with
// the given suffix (":<port>" for a server, ":sync-<pid>-<random>" for a
// sync process). Every process sharing a database needs its own: a process only
// releases the interrupted syncs held under its own identity, so two
// processes with one identity would release each other's live syncs. A
// server's identity survives restarts, so a restarted server releases the
// syncs its previous process left running at once. The result fits the
// owner column; a long hostname is cut, never the distinguishing suffix.
func instanceID(configured, suffix string) string {
	if id := strings.TrimSpace(configured); id != "" {
		if len(id) > store.MaxSyncOwnerLength {
			id = id[:store.MaxSyncOwnerLength]
		}
		return id
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "" // the indexer picks a random one
	}
	if max := store.MaxSyncOwnerLength - len(suffix); len(host) > max {
		host = host[:max]
	}
	return host + suffix
}

// listenSuffix is the instance id suffix of a server: its listen port.
func listenSuffix(listen string) string {
	if _, port, err := net.SplitHostPort(listen); err == nil && port != "" {
		return ":" + port
	}
	return ""
}
