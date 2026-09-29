<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as Empty from '$lib/components/ui/empty';
	import { Button } from '$lib/components/ui/button';
	import PageHeader from '$lib/components/page-header.svelte';
	import RegistriesTable from '$lib/components/registries-table.svelte';
	import ExternalSyncNote from '$lib/components/external-sync-note.svelte';
	import ErrorAlert from '$lib/components/error-alert.svelte';
	import TableSkeleton from '$lib/components/table-skeleton.svelte';
	import { registryClient, type Registry } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { poll } from '$lib/poll.svelte';
	import { startSync, syncPollInterval } from '$lib/sync';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import ServerIcon from '@lucide/svelte/icons/server';

	const registries = new Resource(async () => (await registryClient.listRegistries({})).registries);

	// Poll while any registry is syncing or waits for a syncer.
	poll(() => syncPollInterval(registries.current), registries.poll);

	async function sync(registry: Registry) {
		const updated = await startSync(registry);
		if (updated && registries.current) {
			registries.current = registries.current.map((r) => (r.id === updated.id ? updated : r));
		}
	}
</script>

<PageHeader title="Registries" description="Upstream OCI registries indexed for flatpak images.">
	{#snippet actions()}
		<Button href="/registries/new"><PlusIcon data-icon="inline-start" />Add registry</Button>
	{/snippet}
</PageHeader>

{#if registries.error}
	<ErrorAlert error={registries.error} onretry={registries.refresh} />
{:else if !registries.current}
	<Card.Root><Card.Content><TableSkeleton rows={3} /></Card.Content></Card.Root>
{:else if registries.current.length === 0}
	<Empty.Root class="border border-dashed">
		<Empty.Header>
			<Empty.Media variant="icon"><ServerIcon /></Empty.Media>
			<Empty.Title>No registries yet</Empty.Title>
			<Empty.Description>Connect ghcr.io, registry.fedoraproject.org or any OCI registry.</Empty.Description>
		</Empty.Header>
		<Empty.Content>
			<Button href="/registries/new"><PlusIcon data-icon="inline-start" />Add registry</Button>
		</Empty.Content>
	</Empty.Root>
{:else}
	<Card.Root class="py-0">
		<RegistriesTable registries={registries.current} onsync={sync} />
	</Card.Root>
	<ExternalSyncNote />
{/if}
