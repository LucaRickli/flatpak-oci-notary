import type { Timestamp } from '@bufbuild/protobuf/wkt';
import { RefKind, timestampDate, type Image } from '$lib/api';

export function formatBytes(value: bigint | number): string {
	const n = Number(value);
	if (!n) return '—';
	const units = ['B', 'kB', 'MB', 'GB', 'TB'];
	const i = Math.min(Math.floor(Math.log10(n) / 3), units.length - 1);
	const v = n / 1000 ** i;
	return `${v >= 100 || i === 0 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`;
}

export function formatDuration(ms: bigint | number): string {
	const n = Number(ms);
	if (n < 1000) return `${n} ms`;
	const s = n / 1000;
	if (s < 60) return `${s.toFixed(1)} s`;
	return `${Math.floor(s / 60)} min ${Math.round(s % 60)} s`;
}

export function toDate(ts: Timestamp | undefined): Date | undefined {
	return ts ? timestampDate(ts) : undefined;
}

export function formatDate(ts: Timestamp | undefined): string {
	const d = toDate(ts);
	return d ? d.toLocaleString() : '—';
}

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' });

export function formatRelative(ts: Timestamp | undefined): string {
	const d = toDate(ts);
	if (!d) return 'never';
	const seconds = (d.getTime() - Date.now()) / 1000;
	const steps: [Intl.RelativeTimeFormatUnit, number][] = [
		['second', 60],
		['minute', 60],
		['hour', 24],
		['day', 30],
		['month', 12],
		['year', Infinity]
	];
	let value = seconds;
	for (const [unit, size] of steps) {
		if (Math.abs(value) < size) return rtf.format(Math.round(value), unit);
		value /= size;
	}
	return d.toLocaleDateString();
}

export function formatInterval(minutes: number): string {
	if (!minutes) return 'Manual';
	if (minutes % 1440 === 0) return `Every ${minutes / 1440 === 1 ? 'day' : `${minutes / 1440} days`}`;
	if (minutes % 60 === 0) return `Every ${minutes / 60 === 1 ? 'hour' : `${minutes / 60} hours`}`;
	return `Every ${minutes} min`;
}

export function kindLabel(kind: RefKind): string {
	switch (kind) {
		case RefKind.APP:
			return 'app';
		case RefKind.RUNTIME:
			return 'runtime';
		default:
			return 'unknown';
	}
}

/** Display name of an image: appstream name, falling back to the flatpak id. */
export function imageTitle(image: Pick<Image, 'name' | 'flatpakId' | 'repository'>): string {
	return image.name || image.flatpakId || image.repository;
}

/** Parses a textarea with one entry per line. */
export function parseLines(text: string): string[] {
	return text
		.split(/\r?\n/)
		.map((l) => l.trim())
		.filter(Boolean);
}

export function slugify(text: string): string {
	return text
		.toLowerCase()
		.normalize('NFKD')
		.replace(/[^a-z0-9._-]+/g, '-')
		.replace(/^[^a-z0-9]+/, '')
		.replace(/-+$/, '');
}

export const SLUG_PATTERN = /^[a-z0-9][a-z0-9._-]*$/;

export function initials(name: string): string {
	const parts = name.trim().split(/[\s@._-]+/).filter(Boolean);
	return (parts[0]?.[0] ?? '?').toUpperCase() + (parts[1]?.[0] ?? '').toUpperCase();
}
