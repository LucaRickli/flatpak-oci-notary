<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as Empty from '$lib/components/ui/empty';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import PageHeader from '$lib/components/page-header.svelte';
	import ErrorAlert from '$lib/components/error-alert.svelte';
	import CodeBlock from '$lib/components/code-block.svelte';
	import { registryClient, repositoryClient } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import LibraryIcon from '@lucide/svelte/icons/library';
	import ServerIcon from '@lucide/svelte/icons/server';
	import PackageIcon from '@lucide/svelte/icons/package';
	import ListFilterIcon from '@lucide/svelte/icons/list-filter';

	const repositories = new Resource(async () => (await repositoryClient.listRepositories({})).repositories);
	const registries = new Resource(async () => (await registryClient.listRegistries({})).registries);
	const noRegistries = $derived(registries.current?.length === 0);
</script>

<PageHeader title="Repositories" description="Flatpak remotes published by this server, each selecting images of one registry.">
	{#snippet actions()}
		<Button href="/repositories/new" disabled={noRegistries}><PlusIcon data-icon="inline-start" />New repository</Button>
	{/snippet}
</PageHeader>

{#if repositories.error}
	<ErrorAlert error={repositories.error} onretry={repositories.refresh} />
{:else if !repositories.current}
	<div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
		{#each { length: 3 }, i (i)}<Skeleton class="h-48 rounded-xl" />{/each}
	</div>
{:else if repositories.current.length === 0}
	<Empty.Root class="border border-dashed">
		<Empty.Header>
			<Empty.Media variant="icon"><LibraryIcon /></Empty.Media>
			<Empty.Title>No repositories yet</Empty.Title>
			<Empty.Description>
				{noRegistries
					? 'Add a registry first — every repository serves images from one registry.'
					: 'Create a repository to publish a flatpak remote with a selection of indexed packages.'}
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
	<div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
		{#each repositories.current as repo (repo.id)}
			<Card.Root class="relative">
				<Card.Header>
					<Card.Title class="truncate">
						<a href="/repositories/{repo.id}" class="after:absolute after:inset-0 hover:underline">{repo.title || repo.slug}</a>
					</Card.Title>
					<Card.Description class="line-clamp-2">{repo.description || 'No description'}</Card.Description>
					<Card.Action><Badge variant="outline" class="font-mono">{repo.slug}</Badge></Card.Action>
				</Card.Header>
				<Card.Content class="flex flex-1 flex-col gap-3">
					<div class="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
						<span class="flex items-center gap-1.5"><ServerIcon class="size-3.5" />{repo.registryName}</span>
						<span class="flex items-center gap-1.5"><PackageIcon class="size-3.5" />{repo.imageCount} images</span>
						<span class="flex items-center gap-1.5">
							<ListFilterIcon class="size-3.5" />{repo.sources.length}
							{repo.sources.length === 1 ? 'rule' : 'rules'}
						</span>
					</div>
					{#if repo.urls?.remote}
						<!-- Above the card link overlay so the copy button stays clickable. -->
						<CodeBlock code={repo.urls.remote} class="relative mt-auto" />
					{/if}
				</Card.Content>
			</Card.Root>
		{/each}
	</div>
{/if}
