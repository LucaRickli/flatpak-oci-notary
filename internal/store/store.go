// Package store persists registries, indexed images and repositories in SQLite.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// Store is the SQLite-backed persistence layer.
type Store struct {
	db *sql.DB
	// generation is bumped on every write that can change a served index.
	generation atomic.Uint64
}

// Open opens (and migrates) the database at path.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows a single writer; one connection avoids SQLITE_BUSY churn.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	s.generation.Store(uint64(time.Now().UnixNano()))
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Generation changes whenever data affecting served indexes changes.
func (s *Store) Generation() uint64 { return s.generation.Load() }

func (s *Store) bump() { s.generation.Add(1) }

var migrations = []string{
	`CREATE TABLE registries (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		url TEXT NOT NULL,
		insecure INTEGER NOT NULL DEFAULT 0,
		auth_type TEXT NOT NULL DEFAULT 'anonymous',
		username TEXT NOT NULL DEFAULT '',
		password TEXT NOT NULL DEFAULT '',
		use_catalog INTEGER NOT NULL DEFAULT 0,
		repositories TEXT NOT NULL DEFAULT '[]',
		repository_patterns TEXT NOT NULL DEFAULT '[]',
		tag_patterns TEXT NOT NULL DEFAULT '[]',
		sync_interval_minutes INTEGER NOT NULL DEFAULT 60,
		sync_state TEXT NOT NULL DEFAULT 'never',
		last_sync_at TEXT,
		last_sync_error TEXT NOT NULL DEFAULT '',
		last_sync_duration_ms INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE images (
		id INTEGER PRIMARY KEY,
		registry_id INTEGER NOT NULL REFERENCES registries(id) ON DELETE CASCADE,
		repository TEXT NOT NULL,
		digest TEXT NOT NULL,
		list_digest TEXT NOT NULL DEFAULT '',
		media_type TEXT NOT NULL,
		os TEXT NOT NULL,
		architecture TEXT NOT NULL,
		tags TEXT NOT NULL DEFAULT '[]',
		ref TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		summary TEXT NOT NULL DEFAULT '',
		version TEXT NOT NULL DEFAULT '',
		installed_size INTEGER NOT NULL DEFAULT 0,
		download_size INTEGER NOT NULL DEFAULT 0,
		created TEXT,
		has_icon INTEGER NOT NULL DEFAULT 0,
		labels TEXT NOT NULL DEFAULT '{}',
		indexed_at TEXT NOT NULL,
		UNIQUE (registry_id, repository, digest)
	);
	CREATE INDEX images_registry ON images (registry_id, repository);
	CREATE TABLE repositories (
		id INTEGER PRIMARY KEY,
		slug TEXT NOT NULL UNIQUE,
		title TEXT NOT NULL DEFAULT '',
		description TEXT NOT NULL DEFAULT '',
		homepage TEXT NOT NULL DEFAULT '',
		registry_id INTEGER NOT NULL REFERENCES registries(id) ON DELETE RESTRICT,
		sources TEXT NOT NULL DEFAULT '[]',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`,
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var version int
	err := s.db.QueryRowContext(ctx, `SELECT version FROM schema_version`).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES (0)`); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE schema_version SET version = ?`, i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// helpers

func now() time.Time { return time.Now().UTC() }

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func fmtTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return fmtTime(*t)
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func parseTimePtr(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t := parseTime(s.String)
	return &t
}

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func fromJSON[T any](s string) T {
	var v T
	_ = json.Unmarshal([]byte(s), &v)
	return v
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func isForeignKeyViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}

type scanner interface{ Scan(dest ...any) error }

// ---------------------------------------------------------------------------
// Registries

const registryColumns = `r.id, r.name, r.url, r.insecure, r.auth_type, r.username, r.password, r.use_catalog,
	r.repositories, r.repository_patterns, r.tag_patterns, r.sync_interval_minutes, r.sync_state,
	r.last_sync_at, r.last_sync_error, r.last_sync_duration_ms, r.created_at, r.updated_at,
	(SELECT COUNT(*) FROM images i WHERE i.registry_id = r.id),
	(SELECT COUNT(DISTINCT i.repository) FROM images i WHERE i.registry_id = r.id)`

func scanRegistry(row scanner) (*Registry, error) {
	var (
		r                              Registry
		repos, repoPatterns, tagPats   string
		lastSync                       sql.NullString
		durationMs                     int64
		createdAt, updatedAt, authType string
		state                          string
	)
	err := row.Scan(&r.ID, &r.Name, &r.URL, &r.Insecure, &authType, &r.Username, &r.Password, &r.UseCatalog,
		&repos, &repoPatterns, &tagPats, &r.SyncIntervalMinutes, &state,
		&lastSync, &r.LastSyncError, &durationMs, &createdAt, &updatedAt,
		&r.ImageCount, &r.RepositoryCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.AuthType = AuthType(authType)
	r.SyncState = SyncState(state)
	r.Repositories = nonNil(fromJSON[[]string](repos))
	r.RepositoryPatterns = nonNil(fromJSON[[]string](repoPatterns))
	r.TagPatterns = nonNil(fromJSON[[]string](tagPats))
	r.LastSyncAt = parseTimePtr(lastSync)
	r.LastSyncDuration = time.Duration(durationMs) * time.Millisecond
	r.CreatedAt = parseTime(createdAt)
	r.UpdatedAt = parseTime(updatedAt)
	return &r, nil
}

func (s *Store) ListRegistries(ctx context.Context) ([]*Registry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+registryColumns+` FROM registries r ORDER BY r.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Registry
	for rows.Next() {
		r, err := scanRegistry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetRegistry(ctx context.Context, id int64) (*Registry, error) {
	return scanRegistry(s.db.QueryRowContext(ctx, `SELECT `+registryColumns+` FROM registries r WHERE r.id = ?`, id))
}

func (s *Store) CreateRegistry(ctx context.Context, r *Registry) (*Registry, error) {
	t := fmtTime(now())
	res, err := s.db.ExecContext(ctx, `INSERT INTO registries
		(name, url, insecure, auth_type, username, password, use_catalog, repositories, repository_patterns,
		 tag_patterns, sync_interval_minutes, sync_state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'never', ?, ?)`,
		r.Name, r.URL, r.Insecure, r.AuthType, r.Username, r.Password, r.UseCatalog,
		toJSON(nonNil(r.Repositories)), toJSON(nonNil(r.RepositoryPatterns)), toJSON(nonNil(r.TagPatterns)),
		r.SyncIntervalMinutes, t, t)
	if isUniqueViolation(err) {
		return nil, fmt.Errorf("%w: a registry named %q already exists", ErrConflict, r.Name)
	}
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetRegistry(ctx, id)
}

// UpdateRegistry updates the configuration of a registry. The password is
// only changed when setPassword is true.
func (s *Store) UpdateRegistry(ctx context.Context, r *Registry, setPassword bool) (*Registry, error) {
	q := `UPDATE registries SET name = ?, url = ?, insecure = ?, auth_type = ?, username = ?, use_catalog = ?,
		repositories = ?, repository_patterns = ?, tag_patterns = ?, sync_interval_minutes = ?, updated_at = ?`
	args := []any{r.Name, r.URL, r.Insecure, r.AuthType, r.Username, r.UseCatalog,
		toJSON(nonNil(r.Repositories)), toJSON(nonNil(r.RepositoryPatterns)), toJSON(nonNil(r.TagPatterns)),
		r.SyncIntervalMinutes, fmtTime(now())}
	if setPassword {
		q += `, password = ?`
		args = append(args, r.Password)
	}
	q += ` WHERE id = ?`
	args = append(args, r.ID)
	res, err := s.db.ExecContext(ctx, q, args...)
	if isUniqueViolation(err) {
		return nil, fmt.Errorf("%w: a registry named %q already exists", ErrConflict, r.Name)
	}
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	s.bump()
	return s.GetRegistry(ctx, r.ID)
}

func (s *Store) DeleteRegistry(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM registries WHERE id = ?`, id)
	if isForeignKeyViolation(err) {
		return fmt.Errorf("%w: registry is used by at least one repository", ErrConflict)
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.bump()
	return nil
}

// SetSyncing marks a registry as currently syncing.
func (s *Store) SetSyncing(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE registries SET sync_state = 'syncing' WHERE id = ?`, id)
	return err
}

// FinishSync records the outcome of a sync.
func (s *Store) FinishSync(ctx context.Context, id int64, at time.Time, d time.Duration, syncErr error) error {
	state, msg := SyncOK, ""
	if syncErr != nil {
		state, msg = SyncError, syncErr.Error()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE registries SET sync_state = ?, last_sync_at = ?, last_sync_error = ?,
		last_sync_duration_ms = ? WHERE id = ?`, state, fmtTime(at), msg, d.Milliseconds(), id)
	return err
}

// ResetInterruptedSyncs marks syncs left running by a previous process as failed.
func (s *Store) ResetInterruptedSyncs(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE registries SET sync_state = 'error',
		last_sync_error = 'sync interrupted by server restart' WHERE sync_state = 'syncing'`)
	return err
}

// ---------------------------------------------------------------------------
// Images

const imageColumns = `i.id, i.registry_id, g.name, i.repository, i.digest, i.list_digest, i.media_type, i.os,
	i.architecture, i.tags, i.ref, i.name, i.summary, i.version, i.installed_size, i.download_size, i.created,
	i.has_icon, i.indexed_at`

func scanImage(row scanner, withLabels bool) (*Image, bool, error) {
	var (
		img       Image
		tags      string
		created   sql.NullString
		indexedAt string
		hasIcon   bool
		labels    string
	)
	dest := []any{&img.ID, &img.RegistryID, &img.RegistryName, &img.Repository, &img.Digest, &img.ListDigest,
		&img.MediaType, &img.OS, &img.Architecture, &tags, &img.Ref, &img.Name, &img.Summary, &img.Version,
		&img.InstalledSize, &img.DownloadSize, &created, &hasIcon, &indexedAt}
	if withLabels {
		dest = append(dest, &labels)
	}
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, ErrNotFound
		}
		return nil, false, err
	}
	img.Tags = nonNil(fromJSON[[]string](tags))
	img.Created = parseTimePtr(created)
	img.IndexedAt = parseTime(indexedAt)
	if withLabels {
		img.Labels = fromJSON[map[string]string](labels)
	}
	return &img, hasIcon, nil
}

// ImageFilter restricts ListImages.
type ImageFilter struct {
	RegistryID int64
	Query      string
	// Kind is "app", "runtime" or empty.
	Kind       string
	WithLabels bool
}

// ImageRow is an image plus list-only metadata.
type ImageRow struct {
	*Image
	HasIcon bool
}

func (s *Store) ListImages(ctx context.Context, f ImageFilter) ([]ImageRow, error) {
	cols := imageColumns
	if f.WithLabels {
		cols += `, i.labels`
	}
	q := `SELECT ` + cols + ` FROM images i JOIN registries g ON g.id = i.registry_id WHERE 1 = 1`
	var args []any
	if f.RegistryID != 0 {
		q += ` AND i.registry_id = ?`
		args = append(args, f.RegistryID)
	}
	if f.Kind != "" {
		q += ` AND i.ref LIKE ?`
		args = append(args, f.Kind+"/%")
	}
	if f.Query != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(f.Query) + "%"
		q += ` AND (i.ref LIKE ? ESCAPE '\' OR i.name LIKE ? ESCAPE '\' OR i.repository LIKE ? ESCAPE '\' OR i.summary LIKE ? ESCAPE '\')`
		args = append(args, like, like, like, like)
	}
	q += ` ORDER BY i.ref, i.repository, i.id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ImageRow
	for rows.Next() {
		img, hasIcon, err := scanImage(rows, f.WithLabels)
		if err != nil {
			return nil, err
		}
		out = append(out, ImageRow{Image: img, HasIcon: hasIcon})
	}
	return out, rows.Err()
}

func (s *Store) GetImage(ctx context.Context, id int64) (ImageRow, error) {
	img, hasIcon, err := scanImage(s.db.QueryRowContext(ctx, `SELECT `+imageColumns+`, i.labels
		FROM images i JOIN registries g ON g.id = i.registry_id WHERE i.id = ?`, id), true)
	if err != nil {
		return ImageRow{}, err
	}
	return ImageRow{Image: img, HasIcon: hasIcon}, nil
}

// RepositoryResult is the outcome of indexing one OCI repository.
type RepositoryResult struct {
	Repository string
	Images     []*Image
	// Err is set when the repository could not be indexed; its existing
	// images are kept untouched.
	Err error
}

// ReplaceRegistryImages stores the result of a full registry sync: images of
// successfully indexed repositories are replaced, images of failed
// repositories are kept and images of repositories no longer present are
// removed.
func (s *Store) ReplaceRegistryImages(ctx context.Context, registryID int64, results []RepositoryResult) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	keep := map[string]bool{}
	for _, r := range results {
		keep[r.Repository] = true
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT repository FROM images WHERE registry_id = ?`, registryID)
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var repo string
		if err := rows.Scan(&repo); err != nil {
			rows.Close()
			return err
		}
		if !keep[repo] {
			stale = append(stale, repo)
		}
	}
	rows.Close()
	for _, repo := range stale {
		if _, err := tx.ExecContext(ctx, `DELETE FROM images WHERE registry_id = ? AND repository = ?`, registryID, repo); err != nil {
			return err
		}
	}

	insert, err := tx.PrepareContext(ctx, `INSERT INTO images
		(registry_id, repository, digest, list_digest, media_type, os, architecture, tags, ref, name, summary,
		 version, installed_size, download_size, created, has_icon, labels, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (registry_id, repository, digest) DO UPDATE SET
		 list_digest = excluded.list_digest, media_type = excluded.media_type, os = excluded.os,
		 architecture = excluded.architecture, tags = excluded.tags, ref = excluded.ref, name = excluded.name,
		 summary = excluded.summary, version = excluded.version, installed_size = excluded.installed_size,
		 download_size = excluded.download_size, created = excluded.created, has_icon = excluded.has_icon,
		 labels = excluded.labels, indexed_at = excluded.indexed_at`)
	if err != nil {
		return err
	}
	defer insert.Close()

	for _, r := range results {
		if r.Err != nil {
			continue
		}
		digests := make([]any, 0, len(r.Images))
		for _, img := range r.Images {
			_, err := insert.ExecContext(ctx, registryID, r.Repository, img.Digest, img.ListDigest, img.MediaType,
				img.OS, img.Architecture, toJSON(nonNil(img.Tags)), img.Ref, img.Name, img.Summary, img.Version,
				img.InstalledSize, img.DownloadSize, fmtTimePtr(img.Created), HasIcon(img.Labels),
				toJSON(img.Labels), fmtTime(img.IndexedAt))
			if err != nil {
				return err
			}
			digests = append(digests, img.Digest)
		}
		q := `DELETE FROM images WHERE registry_id = ? AND repository = ?`
		args := []any{registryID, r.Repository}
		if len(digests) > 0 {
			q += ` AND digest NOT IN (?` + strings.Repeat(", ?", len(digests)-1) + `)`
			args = append(args, digests...)
		}
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.bump()
	return nil
}

// IconLabels are the labels carrying appstream icons, best first.
var IconLabels = []string{
	"org.freedesktop.appstream.icon-128",
	"org.freedesktop.appstream.icon-64",
}

// HasIcon reports whether the labels carry an icon.
func HasIcon(labels map[string]string) bool {
	for _, k := range IconLabels {
		if labels[k] != "" {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Repositories

const repositoryColumns = `p.id, p.slug, p.title, p.description, p.homepage, p.registry_id, g.name, p.sources,
	p.created_at, p.updated_at`

func scanRepository(row scanner) (*Repository, error) {
	var (
		p                    Repository
		sources              string
		createdAt, updatedAt string
	)
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Description, &p.Homepage, &p.RegistryID, &p.RegistryName,
		&sources, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Sources = fromJSON[[]Source](sources)
	if p.Sources == nil {
		p.Sources = []Source{}
	}
	p.CreatedAt = parseTime(createdAt)
	p.UpdatedAt = parseTime(updatedAt)
	return &p, nil
}

func (s *Store) ListRepositories(ctx context.Context) ([]*Repository, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+repositoryColumns+`
		FROM repositories p JOIN registries g ON g.id = p.registry_id ORDER BY p.slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Repository
	for rows.Next() {
		p, err := scanRepository(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetRepository(ctx context.Context, id int64) (*Repository, error) {
	return scanRepository(s.db.QueryRowContext(ctx, `SELECT `+repositoryColumns+`
		FROM repositories p JOIN registries g ON g.id = p.registry_id WHERE p.id = ?`, id))
}

func (s *Store) GetRepositoryBySlug(ctx context.Context, slug string) (*Repository, error) {
	return scanRepository(s.db.QueryRowContext(ctx, `SELECT `+repositoryColumns+`
		FROM repositories p JOIN registries g ON g.id = p.registry_id WHERE p.slug = ?`, slug))
}

func (s *Store) CreateRepository(ctx context.Context, p *Repository) (*Repository, error) {
	t := fmtTime(now())
	res, err := s.db.ExecContext(ctx, `INSERT INTO repositories
		(slug, title, description, homepage, registry_id, sources, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Slug, p.Title, p.Description, p.Homepage, p.RegistryID, toJSON(p.Sources), t, t)
	if isUniqueViolation(err) {
		return nil, fmt.Errorf("%w: a repository with slug %q already exists", ErrConflict, p.Slug)
	}
	if isForeignKeyViolation(err) {
		return nil, fmt.Errorf("%w: registry does not exist", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	s.bump()
	return s.GetRepository(ctx, id)
}

func (s *Store) UpdateRepository(ctx context.Context, p *Repository) (*Repository, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE repositories SET slug = ?, title = ?, description = ?, homepage = ?,
		registry_id = ?, sources = ?, updated_at = ? WHERE id = ?`,
		p.Slug, p.Title, p.Description, p.Homepage, p.RegistryID, toJSON(p.Sources), fmtTime(now()), p.ID)
	if isUniqueViolation(err) {
		return nil, fmt.Errorf("%w: a repository with slug %q already exists", ErrConflict, p.Slug)
	}
	if isForeignKeyViolation(err) {
		return nil, fmt.Errorf("%w: registry does not exist", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	s.bump()
	return s.GetRepository(ctx, p.ID)
}

func (s *Store) DeleteRepository(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM repositories WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.bump()
	return nil
}

// ---------------------------------------------------------------------------
// Overview

type Overview struct {
	Registries, Repositories, Images, Apps, Runtimes int
}

func (s *Store) Overview(ctx context.Context) (Overview, error) {
	var o Overview
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM registries),
		(SELECT COUNT(*) FROM repositories),
		(SELECT COUNT(*) FROM images),
		(SELECT COUNT(DISTINCT substr(ref, 1, instr(substr(ref, 5), '/') + 3)) FROM images WHERE ref LIKE 'app/%'),
		(SELECT COUNT(DISTINCT substr(ref, 1, instr(substr(ref, 9), '/') + 7)) FROM images WHERE ref LIKE 'runtime/%')`,
	).Scan(&o.Registries, &o.Repositories, &o.Images, &o.Apps, &o.Runtimes)
	return o, err
}
