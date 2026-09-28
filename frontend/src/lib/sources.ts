import type { Source } from '$lib/api';

/** Editable, plain-object form of a Source rule. */
export interface SourceDraft {
	repositoryPattern: string;
	refPattern: string;
	tagPattern: string;
	exclude: boolean;
}

export function includeEverything(): SourceDraft {
	return { repositoryPattern: '*', refPattern: '*', tagPattern: '*', exclude: false };
}

export function toDraft(s: Source): SourceDraft {
	return {
		repositoryPattern: s.repositoryPattern,
		refPattern: s.refPattern,
		tagPattern: s.tagPattern,
		exclude: s.exclude
	};
}

const isWildcard = (p: string) => p === '' || p === '*' || p === '**';

export function matchesEverything(s: SourceDraft): boolean {
	return isWildcard(s.repositoryPattern) && isWildcard(s.refPattern) && isWildcard(s.tagPattern);
}

export function sameSources(a: SourceDraft[], b: SourceDraft[]): boolean {
	return JSON.stringify(a) === JSON.stringify(b);
}
