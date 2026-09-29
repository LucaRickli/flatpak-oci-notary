package store

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

// ClaimSync takes the sync lease of a registry for owner for the given
// duration. It succeeds when no sync is running or the running sync's lease
// has expired, so exactly one instance syncs a registry at a time. Lease
// times are computed and compared with the database clock, so replicas do
// not need synchronized clocks. Claiming clears a pending sync request and
// resets the progress counters.
func (s *Store) ClaimSync(ctx context.Context, id string, owner string, lease time.Duration) (bool, error) {
	now := s.nowMs()
	res := s.ctx(ctx).Model(&Registry{}).
		Where("id = ? AND (sync_state <> ? OR sync_lease_until < "+now+")", id, SyncSyncing).
		UpdateColumns(map[string]any{
			"sync_state":              SyncSyncing,
			"sync_owner":              owner,
			"sync_lease_until":        gorm.Expr(now+" + ?", lease.Milliseconds()),
			"sync_requested":          false,
			"sync_repositories_done":  0,
			"sync_repositories_total": 0,
		})
	return res.RowsAffected == 1, res.Error
}

// RequestSync records that a sync of the registry was asked for. Any
// syncer (the embedded scheduler, "notary sync --watch" or "notary sync
// --due") treats the registry as due on its next check and clears the flag
// when it claims the sync. A request made while a sync runs is kept, so the
// registry is synced again afterwards.
func (s *Store) RequestSync(ctx context.Context, id string) error {
	res := s.ctx(ctx).Model(&Registry{}).Where("id = ?", id).UpdateColumn("sync_requested", true)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetSyncTotal records how many repositories the running sync will index,
// once they are discovered. It is a no-op (false) when owner no longer
// holds the lease.
func (s *Store) SetSyncTotal(ctx context.Context, id string, owner string, total int) (bool, error) {
	res := s.ctx(ctx).Model(&Registry{}).
		Where("id = ? AND sync_state = ? AND sync_owner = ?", id, SyncSyncing, owner).
		UpdateColumn("sync_repositories_total", total)
	return res.RowsAffected == 1, res.Error
}

// RenewSyncLease extends the lease to the given duration from now while
// owner still holds it. false means the lease was lost (expired and claimed
// by someone else, or the registry is gone) and the sync must stop.
func (s *Store) RenewSyncLease(ctx context.Context, id string, owner string, lease time.Duration) (bool, error) {
	res := s.ctx(ctx).Model(&Registry{}).
		Where("id = ? AND sync_state = ? AND sync_owner = ?", id, SyncSyncing, owner).
		UpdateColumn("sync_lease_until", gorm.Expr(s.nowMs()+" + ?", lease.Milliseconds()))
	return res.RowsAffected == 1, res.Error
}

// maxSyncErrorBytes bounds the stored error message of a sync.
const maxSyncErrorBytes = 4 << 10

// syncErrorMessage makes an error message storable: upstream errors can
// carry raw HTTP bodies with invalid UTF-8 or NUL bytes, which Postgres
// rejects, and can be arbitrarily long.
func syncErrorMessage(err error) string {
	msg := CleanText(err.Error())
	if len(msg) <= maxSyncErrorBytes {
		return msg
	}
	cut := maxSyncErrorBytes
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}
	return msg[:cut] + "…"
}

// FinishSync records the outcome of a sync and releases the lease. It is a
// no-op (false) when owner no longer holds the lease.
func (s *Store) FinishSync(ctx context.Context, id string, owner string, at time.Time, d time.Duration, syncErr error) (bool, error) {
	state, msg := SyncOK, ""
	if syncErr != nil {
		state, msg = SyncError, syncErrorMessage(syncErr)
	}
	at = at.UTC()
	finish := func(msg string) (bool, error) {
		res := s.ctx(ctx).Model(&Registry{}).
			Where("id = ? AND sync_state = ? AND sync_owner = ?", id, SyncSyncing, owner).
			UpdateColumns(map[string]any{
				"sync_state":            state,
				"sync_owner":            "",
				"sync_lease_until":      0,
				"last_sync_at":          &at,
				"last_sync_error":       msg,
				"last_sync_duration_ms": d.Milliseconds(),
			})
		return res.RowsAffected == 1, res.Error
	}
	done, err := finish(msg)
	if err != nil && syncErr != nil && ctx.Err() == nil && !errors.Is(err, context.DeadlineExceeded) {
		// Never leave the registry "syncing" because its error text could
		// not be stored.
		return finish("sync failed (the error message could not be stored: " + CleanText(err.Error()) + ")")
	}
	return done, err
}

// Messages of syncs that stopped before they finished. Registries showing
// one of them are retried on the scheduler's next check.
const (
	// LeaseExpiredError is shown (not stored) for a registry whose syncing
	// instance stopped renewing its lease, e.g. because it crashed.
	LeaseExpiredError = "sync interrupted: the syncing instance stopped renewing its lease"
	// ShutdownInterruptedError is stored when an instance shuts down mid-sync.
	ShutdownInterruptedError = "sync interrupted by server shutdown"
	// RestartInterruptedError is stored at startup for syncs a previous
	// process of this instance left running.
	RestartInterruptedError = "sync interrupted by server restart"
)

// InterruptSync releases owner's lease on a sync that stopped before it
// finished (the instance is shutting down). The attempt is not recorded
// (last_sync_at and the duration are kept) and the registry is marked as
// interrupted, so any instance's scheduler retries it on its next check.
func (s *Store) InterruptSync(ctx context.Context, id string, owner string) (bool, error) {
	res := s.ctx(ctx).Model(&Registry{}).
		Where("id = ? AND sync_state = ? AND sync_owner = ?", id, SyncSyncing, owner).
		UpdateColumns(interruptedColumns(ShutdownInterruptedError))
	return res.RowsAffected == 1, res.Error
}

// ResetInterruptedSyncs marks the syncs that a previous process of the
// given instance (same owner id) left running as interrupted, like
// InterruptSync. Syncs held by other instances are left alone: they may be
// alive (a server and a standalone syncer can share one database, even on
// SQLite), and if they are not, their lease expires.
func (s *Store) ResetInterruptedSyncs(ctx context.Context, owner string) (int64, error) {
	if owner == "" {
		return 0, errors.New("reset interrupted syncs: owner is required")
	}
	res := s.ctx(ctx).Model(&Registry{}).Where("sync_state = ? AND sync_owner = ?", SyncSyncing, owner).
		UpdateColumns(interruptedColumns(RestartInterruptedError))
	return res.RowsAffected, res.Error
}

func interruptedColumns(msg string) map[string]any {
	return map[string]any{
		"sync_state":       SyncError,
		"sync_owner":       "",
		"sync_lease_until": 0,
		"last_sync_error":  msg,
	}
}
