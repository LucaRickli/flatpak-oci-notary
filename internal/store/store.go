// Package store persists registries, indexed images and repositories with
// GORM, on SQLite (default, CGO-free) or Postgres.
package store

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	msqlite "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	// ErrLeaseLost is returned when a sync result is written by an instance
	// that no longer holds the registry's sync lease.
	ErrLeaseLost = errors.New("sync lease lost")
)

// Supported database drivers.
const (
	DriverSQLite   = "sqlite"
	DriverPostgres = "postgres"
)

// DefaultGenerationCacheTTL bounds how long a replica may serve an index
// built before another replica's write.
const DefaultGenerationCacheTTL = time.Second

// Config selects and tunes the database.
type Config struct {
	// Driver is "sqlite" (default) or "postgres".
	Driver string
	// DSN is the Postgres connection URL, or the SQLite file path.
	DSN string
	// Pool settings; zero keeps the database/sql default (unlimited open
	// connections). SQLite always uses a single connection.
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	// GenerationCacheTTL caches the index generation read; 0 means
	// DefaultGenerationCacheTTL, negative disables caching.
	GenerationCacheTTL time.Duration
	Logger             zerolog.Logger
}

// Redacted describes the target without credentials, for logs. A Postgres
// DSN is parsed rather than edited, so a password can never leak through a
// form the redaction does not know (a ?password= parameter, a quoted
// keyword/value, ...): only user, host, port and database are shown.
func (c Config) Redacted() string {
	switch c.Driver {
	case DriverPostgres:
		pc, err := pgconn.ParseConfig(c.DSN)
		if err != nil {
			return "postgres (unparseable dsn)"
		}
		u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(pc.Host, strconv.Itoa(int(pc.Port))), Path: "/" + pc.Database}
		if pc.User != "" {
			u.User = url.User(pc.User)
		}
		if strings.HasPrefix(pc.Host, "/") {
			// Unix socket directory.
			u.Host = ""
			u.RawQuery = url.Values{"host": {pc.Host}, "port": {strconv.Itoa(int(pc.Port))}}.Encode()
		}
		return u.String()
	default:
		return c.DSN
	}
}

// Store is the persistence layer.
type Store struct {
	Driver string
	db     *gorm.DB

	genTTL time.Duration
	genMu  sync.Mutex
	gen    uint64
	genAt  time.Time
	// genSeq counts stores of gen, so a database read that raced with a
	// newer local value does not overwrite it.
	genSeq uint64
}

// Open connects to the database and applies pending migrations.
func Open(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Driver == "" {
		cfg.Driver = DriverSQLite
	}
	var dialector gorm.Dialector
	switch cfg.Driver {
	case DriverSQLite:
		if cfg.DSN == "" {
			return nil, errors.New("sqlite: database path is required")
		}
		path := strings.TrimPrefix(cfg.DSN, "file:")
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
		// One writer, WAL, foreign keys, immediate transactions (so a
		// transaction never has to upgrade a read lock and fail with
		// SQLITE_BUSY), ISO-8601 timestamps.
		dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)" +
			"&_pragma=synchronous(NORMAL)&_txlock=immediate&_time_format=sqlite"
		dialector = sqlite.New(sqlite.Config{DriverName: "sqlite", DSN: dsn})
	case DriverPostgres:
		if cfg.DSN == "" {
			return nil, errors.New("postgres: database.dsn is required")
		}
		dialector = postgres.New(postgres.Config{DSN: cfg.DSN})
	default:
		return nil, fmt.Errorf("unknown database driver %q (sqlite or postgres)", cfg.Driver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger:                 &zlog{log: cfg.Logger.With().Str("component", "db").Logger()},
		NowFunc:                func() time.Time { return time.Now().UTC() },
		SkipDefaultTransaction: true,
		NamingStrategy:         schema.NamingStrategy{},
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	switch cfg.Driver {
	case DriverSQLite:
		// SQLite allows a single writer; one connection avoids SQLITE_BUSY churn.
		sqlDB.SetMaxOpenConns(1)
	default:
		if cfg.MaxOpenConns > 0 {
			sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
		}
		if cfg.MaxIdleConns > 0 {
			sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
		}
		if cfg.ConnMaxLifetime > 0 {
			sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
		}
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("connect: %w", err)
	}

	s := &Store{Driver: cfg.Driver, db: db, genTTL: cfg.GenerationCacheTTL}
	if s.genTTL == 0 {
		s.genTTL = DefaultGenerationCacheTTL
	}
	if err := s.migrate(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// DB exposes the underlying GORM handle, for tests.
func (s *Store) DB() *gorm.DB { return s.db }

func (s *Store) ctx(ctx context.Context) *gorm.DB { return s.db.WithContext(ctx) }

// ---------------------------------------------------------------------------
// Index generation

// Generation changes whenever data affecting served indexes changes, on any
// replica sharing the database. The value may be cached for up to the
// configured TTL; this instance's own writes are visible immediately.
func (s *Store) Generation(ctx context.Context) (uint64, error) {
	s.genMu.Lock()
	if s.genTTL > 0 && !s.genAt.IsZero() && time.Since(s.genAt) < s.genTTL {
		g := s.gen
		s.genMu.Unlock()
		return g, nil
	}
	seq := s.genSeq
	s.genMu.Unlock()

	// Bounded, so a hung database cannot stall requests that the caller
	// could answer from the last known generation (see LastGeneration).
	rctx, cancel := context.WithTimeout(ctx, generationReadTimeout)
	defer cancel()
	var st indexState
	if err := s.ctx(rctx).First(&st, 1).Error; err != nil {
		return 0, err
	}
	g := uint64(st.Generation)

	s.genMu.Lock()
	defer s.genMu.Unlock()
	if s.genSeq != seq {
		// A local write (or a concurrent read) stored a value while this
		// read was in flight; it is at least as recent as what we read
		// from the caller's point of view.
		return s.gen, nil
	}
	s.gen, s.genAt = g, time.Now()
	s.genSeq++
	return g, nil
}

// generationReadTimeout bounds the database read in Generation.
const generationReadTimeout = 2 * time.Second

// LastGeneration returns the last generation this instance read or wrote,
// and false if it never knew one. Callers may use it to keep serving cached
// data while the database is unreachable (no replica can write then).
func (s *Store) LastGeneration() (uint64, bool) {
	s.genMu.Lock()
	defer s.genMu.Unlock()
	return s.gen, !s.genAt.IsZero()
}

// setGeneration stores the generation of a local write after its commit.
func (s *Store) setGeneration(g uint64) {
	s.genMu.Lock()
	s.gen, s.genAt = g, time.Now()
	s.genSeq++
	s.genMu.Unlock()
}

// bumpGeneration increments the shared counter inside tx. The caller stores
// the returned value with setGeneration after the transaction committed.
func bumpGeneration(tx *gorm.DB) (uint64, error) {
	if err := tx.Model(&indexState{}).Where("id = ?", 1).UpdateColumn("generation", gorm.Expr("generation + 1")).Error; err != nil {
		return 0, err
	}
	var st indexState
	if err := tx.First(&st, 1).Error; err != nil {
		return 0, err
	}
	return uint64(st.Generation), nil
}

// bumping runs fn in a transaction that also bumps the index generation.
func (s *Store) bumping(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return s.bumpingWhen(ctx, func(tx *gorm.DB) (bool, error) { return true, fn(tx) })
}

// bumpingWhen runs fn in a transaction and bumps the index generation when
// fn reports that it changed data affecting served indexes.
func (s *Store) bumpingWhen(ctx context.Context, fn func(tx *gorm.DB) (bool, error)) error {
	var gen uint64
	err := s.ctx(ctx).Transaction(func(tx *gorm.DB) error {
		bump, err := fn(tx)
		if err != nil || !bump {
			return err
		}
		gen, err = bumpGeneration(tx)
		return err
	})
	if err != nil {
		return err
	}
	if gen > 0 {
		s.setGeneration(gen)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Errors

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	var sqErr *msqlite.Error
	if errors.As(err, &sqErr) {
		return sqErr.Code() == 2067 || sqErr.Code() == 1555 // SQLITE_CONSTRAINT_UNIQUE, _PRIMARYKEY
	}
	return false
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	var sqErr *msqlite.Error
	if errors.As(err, &sqErr) {
		// SQLITE_CONSTRAINT_FOREIGNKEY; ON DELETE RESTRICT surfaces as
		// SQLITE_CONSTRAINT_TRIGGER with the same message.
		return sqErr.Code() == 787 || (sqErr.Code() == 1811 && strings.Contains(sqErr.Error(), "FOREIGN KEY"))
	}
	return false
}

func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

// ---------------------------------------------------------------------------
// Pagination

const (
	DefaultPageSize = 50
	MaxPageSize     = 500
)

// Page selects a window of a listing. A zero Size means DefaultPageSize,
// sizes above MaxPageSize are clamped and negative offsets mean 0.
type Page struct {
	Size   int
	Offset int
}

// Normalize applies the defaults and limits.
func (p Page) Normalize() Page {
	if p.Size <= 0 {
		p.Size = DefaultPageSize
	}
	if p.Size > MaxPageSize {
		p.Size = MaxPageSize
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p
}

// Slice applies the page to an in-memory result.
func Slice[T any](items []T, p Page) []T {
	p = p.Normalize()
	if p.Offset >= len(items) {
		return []T{}
	}
	end := min(p.Offset+p.Size, len(items))
	return items[p.Offset:end]
}

// likePattern builds a substring pattern for
// `LOWER(col) LIKE LOWER(?) ESCAPE '\'`, escaping LIKE metacharacters. Both
// sides are folded by the database, so they always agree: SQLite folds
// ASCII only (a query matches its exact case and ASCII case variants),
// Postgres folds according to the database locale.
func likePattern(q string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
}

// likeEscape is the ESCAPE clause, valid on both SQLite and Postgres.
const likeEscape = ` ESCAPE '\'`

// likeAny builds a condition matching the pattern against any of cols.
func likeAny(q string, cols ...string) (string, []any) {
	like := likePattern(q)
	conds := make([]string, len(cols))
	args := make([]any, len(cols))
	for i, c := range cols {
		conds[i] = "LOWER(" + c + ") LIKE LOWER(?)" + likeEscape
		args[i] = like
	}
	return strings.Join(conds, " OR "), args
}

// orderText orders a text column by byte value on every dialect (SQLite's
// default; Postgres would otherwise use the database collation), so
// listings are ordered the same everywhere.
func (s *Store) orderText(col string) string {
	if s.Driver == DriverPostgres {
		return col + ` COLLATE "C"`
	}
	return col
}

// nowMs is the database clock in Unix milliseconds. Leases are compared
// with it, so replicas do not need synchronized clocks.
func (s *Store) nowMs() string {
	if s.Driver == DriverPostgres {
		return "(EXTRACT(EPOCH FROM clock_timestamp()) * 1000)::bigint"
	}
	return "CAST(unixepoch('subsec') * 1000 AS INTEGER)"
}

// CleanText makes a string from an untrusted source storable on every
// dialect: invalid UTF-8 is replaced and NUL bytes (rejected by Postgres
// text columns) are removed.
func CleanText(s string) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "")
	}
	return s
}

// ---------------------------------------------------------------------------
// Logging

// zlog adapts GORM's logger to zerolog. Bound parameters are never logged.
type zlog struct{ log zerolog.Logger }

const slowQuery = 500 * time.Millisecond

func (l *zlog) LogMode(gormlogger.LogLevel) gormlogger.Interface { return l }
func (l *zlog) Info(_ context.Context, msg string, args ...any) {
	l.log.Info().Msgf(msg, args...)
}
func (l *zlog) Warn(_ context.Context, msg string, args ...any) {
	l.log.Warn().Msgf(msg, args...)
}
func (l *zlog) Error(_ context.Context, msg string, args ...any) {
	l.log.Error().Msgf(msg, args...)
}

func (l *zlog) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	d := time.Since(begin)
	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && !errors.Is(err, context.Canceled):
		sql, rows := fc()
		l.log.Debug().Err(err).Dur("duration", d).Int64("rows", rows).Str("sql", sql).Msg("query failed")
	case d > slowQuery:
		sql, rows := fc()
		l.log.Warn().Dur("duration", d).Int64("rows", rows).Str("sql", sql).Msg("slow query")
	}
}

// ParamsFilter strips bound parameters (which may include passwords) from
// logged SQL.
func (l *zlog) ParamsFilter(_ context.Context, sql string, _ ...any) (string, []any) {
	return sql, nil
}
