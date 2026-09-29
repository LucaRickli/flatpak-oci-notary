import type { Repository } from '$lib/api';

/** Command that adds a repository as a flatpak remote (named after its slug). */
export function remoteAddCommand(repo: Pick<Repository, 'slug' | 'urls'>): string {
	return `flatpak remote-add --if-not-exists --no-gpg-verify ${repo.slug} ${repo.urls?.remote ?? ''}`;
}
