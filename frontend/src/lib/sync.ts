import { toast } from 'svelte-sonner';
import { registryClient, type Registry } from '$lib/api';
import { reportError } from '$lib/session.svelte';

/** Starts a background sync; returns the updated registry (state "syncing"). */
export async function startSync(registry: Pick<Registry, 'id' | 'name'>): Promise<Registry | undefined> {
	try {
		const res = await registryClient.syncRegistry({ id: registry.id });
		toast.success(`Sync of ${registry.name} started`);
		return res.registry;
	} catch (err) {
		reportError(err, 'Could not start sync');
	}
}
