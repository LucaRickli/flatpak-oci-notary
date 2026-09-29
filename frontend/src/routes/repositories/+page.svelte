<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as Empty from '$lib/components/ui/empty';
	import { Button } from '$lib/components/ui/button';
	import PageHeader from '$lib/components/page-header.svelte';
	import ErrorAlert from '$lib/components/error-alert.svelte';
	import TableSkeleton from '$lib/components/table-skeleton.svelte';
	import RepositoriesTable from '$lib/components/repositories-table.svelte';
	import { registryClient, repositoryClient } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import LibraryIcon from '@lucide/svelte/icons/library';

	const repositories = new Resource(async () => (await repositoryClient.listRepositories({})).repositories);
	const registries = new Resource(async () => (await registryClient.listRegistries({})).registries);
	const noRegistries = $derived(registries.current?.length === 0);
</script>

<PageHeader title="Repositories" description="Flatpak remotes published by this server.">
	{#snippet actions()}
		<Button href="/repositories/new" disabled={noRegistries}><PlusIcon data-icon="inline-start" />New repository</Button>
	{/snippet}
</PageHeader>

{#if repositories.error}
	<ErrorAlert error={repositories.error} onretry={repositories.refresh} />
{:else if !repositories.current}
	<Card.Root><Card.Content><TableSkeleton rows={3} /></Card.Content></Card.Root>
{:else if repositories.current.length === 0}
	<Empty.Root class="border border-dashed">
		<Empty.Header>
			<Empty.Media variant="icon"><LibraryIcon /></Empty.Media>
			<Empty.Title>No repositories yet</Empty.Title>
			<Empty.Description>
				{noRegistries
					? 'Add a registry first — every repository serves images from one registry.'
					: 'Publish a flatpak remote with a selection of indexed packages.'}
			</Empty.Description>
		</Empty.Header>
		<Empty.Content>
			{#if noRegistries}
				<Button href="/registries/new"><PlusIcon data-icon="inline-start" />Add registry</Button>
			{:else}
				<Button href="/repositories/new"><PlusIcon data-icon="inline-start" />New repository</Button>
			{/if}
		</Empty.Content>
	</Empty.Root>
{:else}
	<Card.Root class="py-0">
		<RepositoriesTable repositories={repositories.current} />
	</Card.Root>
{/if}
