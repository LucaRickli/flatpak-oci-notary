import { RefKind, type Image } from '$lib/api';

/** Kind as used in package URLs (/packages/app/…, /packages/runtime/…). */
export type KindSlug = 'app' | 'runtime';

export function kindSlug(kind: RefKind): KindSlug | undefined {
	if (kind === RefKind.APP) return 'app';
	if (kind === RefKind.RUNTIME) return 'runtime';
	return undefined;
}

export function parseKindSlug(slug: string): RefKind | undefined {
	if (slug === 'app') return RefKind.APP;
	if (slug === 'runtime') return RefKind.RUNTIME;
	return undefined;
}

/** Query parameter of the package page that selects a variant (an image id). */
export const VARIANT_PARAM = 'variant';

/** Package page; `variantId` preselects one of its images. */
export function packageHref(kind: RefKind, flatpakId: string, variantId?: string): string {
	const slug = kindSlug(kind);
	if (!slug || !flatpakId) return variantId ? `/images/${variantId}` : '/packages';
	const base = `/packages/${slug}/${encodeURIComponent(flatpakId)}`;
	return variantId ? `${base}?${new URLSearchParams({ [VARIANT_PARAM]: variantId })}` : base;
}

/** Page of one image: its package with the image selected. */
export function imageHref(image: Pick<Image, 'id' | 'kind' | 'flatpakId'>): string {
	return packageHref(image.kind, image.flatpakId, image.id);
}

/**
 * Package page of a flatpak ref as found in flatpak metadata: "runtime/<id>/<arch>/<branch>",
 * "app/<id>/…", or without kind ("org.fedoraproject.Platform/x86_64/f44"), which is `defaultKind`.
 */
export function refHref(ref: string, defaultKind: RefKind = RefKind.RUNTIME): string | undefined {
	const parts = ref.split('/');
	const kind = parseKindSlug(parts[0]);
	const id = kind === undefined ? parts[0] : parts[1];
	return id ? packageHref(kind ?? defaultKind, id) : undefined;
}
