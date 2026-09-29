import { untrack } from 'svelte';
import type { Registry } from '$lib/api';
import { syncEnded, syncSnapshot, type SyncSnapshot } from '$lib/sync';

/**
 * Calls `fn` every `interval()` milliseconds while `interval()` returns a
 * positive number; undefined or 0 stops polling. `interval` is reactive: when
 * it changes (or any state it reads does), the timer restarts. Ticks are
 * skipped while the page is hidden, and `fn` runs once when it becomes
 * visible again. Must be called during component initialisation.
 */
export function poll(interval: () => number | undefined, fn: () => unknown) {
	$effect(() => {
		const ms = interval();
		if (!ms) return;
		const tick = () => {
			if (!document.hidden) fn();
		};
		const visible = () => {
			if (!document.hidden) fn();
		};
		const timer = setInterval(tick, ms);
		document.addEventListener('visibilitychange', visible);
		return () => {
			clearInterval(timer);
			document.removeEventListener('visibilitychange', visible);
		};
	});
}

/**
 * Calls `fn` for every registry whose sync ended since the previous value of
 * `registries()` (see syncEnded), e.g. to refresh lists and report the
 * outcome. Registries seen for the first time are only recorded. Must be
 * called during component initialisation.
 */
export function onSyncEnded(registries: () => Registry[] | undefined, fn: (registry: Registry) => void) {
	const seen = new Map<string, SyncSnapshot>();
	$effect(() => {
		const list = registries();
		if (!list) return;
		untrack(() => {
			for (const r of list) {
				const next = syncSnapshot(r);
				const prev = seen.get(r.id);
				seen.set(r.id, next);
				if (prev && syncEnded(prev, next)) fn(r);
			}
		});
	});
}
