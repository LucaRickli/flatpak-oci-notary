<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Command from '$lib/components/ui/command';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { Button } from '$lib/components/ui/button';
	import { Spinner } from '$lib/components/ui/spinner';
	import PackageIcon from './package-icon.svelte';
	import KindBadge from './kind-badge.svelte';
	import ErrorAlert from './error-alert.svelte';
	import TablePagination from './table-pagination.svelte';
	import { imageClient, type ImageRepository } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { imageTitle } from '$lib/format';
	import { pageRequest } from '$lib/pagination';
	import { cn } from '$lib/utils/shadcn';
	import type { SourceDraft } from '$lib/sources';

	let {
		open = $bindable(false),
		registryId,
		sources,
		onpick
	}: {
		open?: boolean;
		registryId: string;
		/** Current rules, to mark packages that already have a rule. */
		sources: SourceDraft[];
		onpick: (rule: SourceDraft) => void;
	} = $props();

	const PAGE_SIZE = 50;

	let mode = $state<'include' | 'exclude'>('include');
	let search = $state('');
	let query = $state('');

	$effect(() => {
		const q = search.trim();
		const timer = setTimeout(() => (query = q), 250);
		return () => clearTimeout(timer);
	});

	// Overridable; goes back to the first page on a new search.
	let page = $derived.by(() => {
		void query;
		void registryId;
		return 1;
	});

	// One entry per OCI repository; a rule selects all of its arches and tags. Loaded up front so
	// the list (and its search input) is ready when the dialog opens.
	const packages = new Resource(() =>
		imageClient.listImageRepositories({ registryId, query, ...pageRequest(page, PAGE_SIZE) })
	);

	// Refresh in the background on every open, in case a sync added packages.
	$effect(() => {
		if (open) packages.refresh();
	});

	/** The fields imageTitle needs; the name falls back to the first flatpak ID. */
	function display(repo: ImageRepository) {
		return { name: repo.name, flatpakId: repo.flatpakIds[0] ?? '', repository: repo.repository };
	}

	function hasRule(repository: string) {
		return sources.some((s) => s.repositoryPattern === repository && s.exclude === (mode === 'exclude'));
	}

	function pick(repo: ImageRepository) {
		if (hasRule(repo.repository)) return;
		onpick({ repositoryPattern: repo.repository, refPattern: '*', tagPattern: '*', exclude: mode === 'exclude' });
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="gap-0 p-0 sm:max-w-xl">
		<Dialog.Header class="p-4 pb-3">
			<Dialog.Title>Add package</Dialog.Title>
			<Dialog.Description>
				Adds a rule matching the package's OCI repository exactly, with all its refs, arches and tags.
			</Dialog.Description>
		</Dialog.Header>
		{#if packages.error}
			<div class="px-4 pb-4"><ErrorAlert error={packages.error} onretry={packages.refresh} /></div>
		{:else if !packages.current}
			<div class="flex justify-center p-8"><Spinner class="size-5 text-muted-foreground" /></div>
		{:else}
			{@const total = packages.current.totalSize}
			<!-- Searched on the server, so the command list must not filter on its own. -->
			<Command.Root shouldFilter={false} class="rounded-none! border-t">
				<Command.Input placeholder="Search packages…" bind:value={search} />
				<Command.List class={cn('max-h-96 transition-opacity', packages.loading && 'opacity-60')}>
					<Command.Empty>
						{query ? 'No packages found.' : 'No packages indexed for this registry yet.'}
					</Command.Empty>
					{#if total > 0}
						<Command.Group heading="{total.toLocaleString()} {total === 1 ? 'package' : 'packages'}">
							{#each packages.current.repositories as repo (repo.repository)}
								{@const entry = display(repo)}
								{@const title = imageTitle(entry)}
								{@const ids = repo.flatpakIds.join(', ')}
								<Command.Item
									value={repo.repository}
									data-checked={hasRule(repo.repository)}
									onSelect={() => pick(repo)}
								>
									<PackageIcon iconId={repo.iconImageId} alt={title} class="size-8" />
									<div class="flex min-w-0 flex-1 flex-col">
										<span class="truncate font-medium">{title}</span>
										<span class="truncate font-mono text-xs text-muted-foreground">{repo.repository}</span>
										{#if ids && ids !== title}
											<span class="truncate font-mono text-xs text-muted-foreground">{ids}</span>
										{/if}
									</div>
									<div class="hidden shrink-0 flex-col items-end text-xs text-muted-foreground sm:flex">
										<span class="font-mono">{repo.architectures.join(', ')}</span>
										<span>{repo.imageCount} {repo.imageCount === 1 ? 'image' : 'images'}</span>
									</div>
									<KindBadge kind={repo.kind} />
								</Command.Item>
							{/each}
						</Command.Group>
					{/if}
				</Command.List>
			</Command.Root>
			{#if total > PAGE_SIZE || page > 1}
				<div class="border-t px-3 py-2">
					<TablePagination {total} bind:page pageSize={PAGE_SIZE} pageSizes={[]} />
				</div>
			{/if}
		{/if}
		<Dialog.Footer class="m-0 flex-row items-center justify-between p-3 sm:justify-between">
			<ToggleGroup.Root
				type="single"
				size="sm"
				variant="outline"
				value={mode}
				onValueChange={(v) => v && (mode = v as 'include' | 'exclude')}
				aria-label="Rule type"
			>
				<ToggleGroup.Item value="include">Include</ToggleGroup.Item>
				<ToggleGroup.Item value="exclude">Exclude</ToggleGroup.Item>
			</ToggleGroup.Root>
			<Button onclick={() => (open = false)}>Done</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
