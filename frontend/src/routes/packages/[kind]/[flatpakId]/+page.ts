import { error } from '@sveltejs/kit';
import { parseKindSlug } from '$lib/links';
import type { PageLoad } from './$types';

export const load: PageLoad = ({ params }) => {
	const kind = parseKindSlug(params.kind);
	if (kind === undefined || !params.flatpakId) error(404, 'Not found');
	return { kind, flatpakId: params.flatpakId };
};
