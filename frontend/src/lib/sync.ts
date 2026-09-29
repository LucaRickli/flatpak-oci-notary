import { toast } from 'svelte-sonner';
import { registryClient, SyncState, type Registry } from '$lib/api';
import { toDate } from '$lib/format';
import { reportError, session } from '$lib/session.svelte';

/**
 * Poll intervals (ms): registry state while a sync runs, while one waits for a syncer, while
 * nothing is pending (to notice syncs started elsewhere: the scheduler, a `notary sync` job or
 * another admin), and lists that grow during a sync.
 */
export const POLL_SYNCING_MS = 2000;
export const POLL_QUEUED_MS = 5000;
export const POLL_IDLE_MS = 20000;
export const POLL_LIST_MS = 5000;

type SyncFields = Pick<Registry, 'syncState' | 'syncRequested'>;

export function isSyncing(registry: SyncFields): boolean {
	return registry.syncState === SyncState.SYNCING;
}

/** A sync was requested and waits for a syncer to pick it up. */
export function isQueued(registry: SyncFields): boolean {
	return registry.syncRequested && registry.syncState !== SyncState.SYNCING;
}

/** A sync is running and another one was requested after it started: it runs next. */
export function isQueuedBehind(registry: SyncFields): boolean {
	return registry.syncRequested && registry.syncState === SyncState.SYNCING;
}

/** How often to poll the registry state; undefined until the registries are loaded. */
export function syncPollInterval(registries: SyncFields[] | undefined): number | undefined {
	if (!registries) return undefined;
	if (registries.some(isSyncing)) return POLL_SYNCING_MS;
	if (registries.some(isQueued)) return POLL_QUEUED_MS;
	return POLL_IDLE_MS;
}

/** What `syncEnded` compares between two observations of a registry. */
export interface SyncSnapshot {
	state: SyncState;
	requested: boolean;
	lastSyncAt: number | undefined;
}

export function syncSnapshot(registry: Pick<Registry, 'syncState' | 'syncRequested' | 'lastSyncAt'>): SyncSnapshot {
	return {
		state: registry.syncState,
		requested: registry.syncRequested,
		lastSyncAt: toDate(registry.lastSyncAt)?.getTime()
	};
}

/**
 * Whether a sync ended between two observations of a registry. Syncs can start and finish
 * between two polls (a small registry, or a resync reusing manifests), so besides a sync seen
 * running this counts a new last-sync time, a queued request that went away and a changed
 * outcome (an interrupted sync keeps its last-sync time) while nothing runs.
 */
export function syncEnded(prev: SyncSnapshot, next: SyncSnapshot): boolean {
	if (next.state === SyncState.SYNCING) return false;
	return (
		prev.state === SyncState.SYNCING ||
		prev.lastSyncAt !== next.lastSyncAt ||
		prev.state !== next.state ||
		(prev.requested && !next.requested)
	);
}

/**
 * Whether this server runs syncs itself. When false, a separate `notary sync`
 * process does and "Sync now" only records a request. Unknown (GetInfo failed)
 * counts as embedded, the default setup.
 */
export function embeddedSync(): boolean {
	return session.info?.embeddedSync ?? true;
}

/** Label and explanation of the "sync now" action for the current setup. */
export function syncAction(registry?: SyncFields): { label: string; hint: string } {
	if (registry && isQueuedBehind(registry)) {
		return { label: 'Syncing…', hint: 'A sync is running; another one is queued to run after it.' };
	}
	if (registry && isSyncing(registry)) return { label: 'Syncing…', hint: 'A sync is running.' };
	if (registry && isQueued(registry)) {
		return {
			label: 'Sync queued',
			hint: embeddedSync()
				? 'The sync starts shortly.'
				: 'Waiting for the external syncer (notary sync) to pick it up.'
		};
	}
	return embeddedSync()
		? { label: 'Sync now', hint: 'Index the registry now.' }
		: {
				label: 'Request sync',
				hint: 'Syncs run in a separate “notary sync” process; it picks up the request on its next run.'
			};
}

/**
 * Starts a background sync (embedded syncer) or records a request for the
 * external syncer; returns the updated registry.
 */
export async function startSync(registry: Pick<Registry, 'id' | 'name'>): Promise<Registry | undefined> {
	try {
		const res = await registryClient.syncRegistry({ id: registry.id });
		const updated = res.registry;
		if (updated && isQueuedBehind(updated)) {
			toast.success(`A sync of ${registry.name} is already running`, {
				description: 'Yours is queued and runs after it.'
			});
		} else if (updated && isSyncing(updated)) {
			toast.success(`Sync of ${registry.name} started`);
		} else if (!embeddedSync()) {
			toast.success(`Sync of ${registry.name} requested`, {
				description: 'The external syncer (notary sync) picks it up on its next run.'
			});
		} else {
			toast.success(`Sync of ${registry.name} queued`);
		}
		return updated;
	} catch (err) {
		reportError(err, embeddedSync() ? 'Could not start sync' : 'Could not request sync');
	}
}
