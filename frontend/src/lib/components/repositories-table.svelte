<script lang="ts">
	import { goto } from '$app/navigation';
	import * as Table from '$lib/components/ui/table';
	import CopyButton from './copy-button.svelte';
	import type { Repository } from '$lib/api';
	import { formatDate, formatRelative } from '$lib/format';
	import { remoteAddCommand } from '$lib/remote';
	import { rowClick } from '$lib/row-link';
	import LibraryIcon from '@lucide/svelte/icons/library';

	let { repositories, compact = false }: { repositories: Repository[]; /** Fewer columns (dashboard). */ compact?: boolean } =
		$props();
</script>

<Table.Root>
	<Table.Header>
		<Table.Row class="hover:bg-transparent">
			<Table.Head class="pl-4">Repository</Table.Head>
			{#if !compact}<Table.Head class="hidden md:table-cell">Registry</Table.Head>{/if}
			<Table.Head class="hidden text-right sm:table-cell">Images</Table.Head>
			{#if !compact}
				<Table.Head class="hidden text-right lg:table-cell">Rules</Table.Head>
				<Table.Head class="hidden lg:table-cell">Updated</Table.Head>
			{/if}
			<Table.Head class="w-12 pr-3"><span class="sr-only">Copy remote-add command</span></Table.Head>
		</Table.Row>
	</Table.Header>
	<Table.Body>
		{#each repositories as repo (repo.id)}
			<Table.Row class="h-16 cursor-pointer" onclick={rowClick(() => goto(`/repositories/${repo.id}`))}>
				<Table.Cell class="max-w-52 pl-4 sm:max-w-96">
					<div class="flex items-center gap-3">
						<div
							class="hidden size-9 shrink-0 items-center justify-center rounded-lg bg-success/12 text-success sm:flex"
						>
							<LibraryIcon class="size-4" />
						</div>
						<div class="flex min-w-0 flex-col">
							<a href="/repositories/{repo.id}" class="truncate font-medium hover:underline">{repo.title || repo.slug}</a>
							<span class="truncate text-xs text-muted-foreground">
								<span class="font-mono">{repo.slug}</span>{repo.description ? ` · ${repo.description}` : ''}
							</span>
						</div>
					</div>
				</Table.Cell>
				{#if !compact}
					<Table.Cell class="hidden md:table-cell">
						<a href="/registries/{repo.registryId}" class="text-muted-foreground hover:text-foreground hover:underline">
							{repo.registryName}
						</a>
					</Table.Cell>
				{/if}
				<Table.Cell class="hidden text-right font-medium tabular-nums sm:table-cell">
					{repo.imageCount.toLocaleString()}
				</Table.Cell>
				{#if !compact}
					<Table.Cell class="hidden text-right tabular-nums lg:table-cell">
						{repo.sources.length}
						{#if repo.sources.some((s) => s.exclude)}
							<span class="text-xs text-muted-foreground">({repo.sources.filter((s) => s.exclude).length} excl.)</span>
						{/if}
					</Table.Cell>
					<Table.Cell class="hidden text-muted-foreground lg:table-cell">
						<span title={formatDate(repo.updatedAt)}>{formatRelative(repo.updatedAt)}</span>
					</Table.Cell>
				{/if}
				<Table.Cell class="pr-3 text-right">
					{#if repo.urls?.remote}
						<CopyButton value={remoteAddCommand(repo)} label="Copy remote-add command for {repo.slug}" />
					{/if}
				</Table.Cell>
			</Table.Row>
		{/each}
	</Table.Body>
</Table.Root>
