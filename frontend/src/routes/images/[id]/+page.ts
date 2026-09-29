import { error } from '@sveltejs/kit';
import { canonicalId } from '$lib/ids';
import type { PageLoad } from './$types';

export const load: PageLoad = ({ params }) => {
	// Canonical form: the API answers with lowercase ids, which the page compares against.
	const id = canonicalId(params.id);
	if (!id) error(404, 'Not found');
	return { id };
};
