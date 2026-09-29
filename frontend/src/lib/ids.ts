/** UUID string in any case (the API uses UUIDv7 for every id and accepts either case). */
const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Whether `value` looks like an API id; use it to validate route params and URL filters. */
export function isId(value: string | null | undefined): value is string {
	return !!value && UUID_PATTERN.test(value);
}

/**
 * The canonical (lowercase) form of an id from a URL, as the API returns it, or undefined when
 * `value` is not an id. Compare ids from URLs with API ids only in this form.
 */
export function canonicalId(value: string | null | undefined): string | undefined {
	return isId(value) ? value.toLowerCase() : undefined;
}
