import { page } from '$app/state';

/**
 * Label for the last breadcrumb of a detail page (e.g. a registry name).
 * Keyed by path, so a stale label never shows on another page.
 */
export const crumb = $state({ path: '', label: '' });

/** Call from a detail page (inside an effect) once its entity is loaded. */
export function setCrumb(label: string) {
	crumb.path = page.url.pathname;
	crumb.label = label;
}
