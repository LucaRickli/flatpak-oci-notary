<script lang="ts">
	import * as Table from '$lib/components/ui/table';
	import { Badge } from '$lib/components/ui/badge';
	import ExtraDataBadge from './metadata/extra-data-badge.svelte';
	import type { Image } from '$lib/api';
	import { formatBytes, shortDigest } from '$lib/format';
	import { cn } from '$lib/utils/shadcn';
	import CircleIcon from '@lucide/svelte/icons/circle';
	import CircleDotIcon from '@lucide/svelte/icons/circle-dot';

	let {
		variants,
		selectedId,
		onselect
	}: {
		variants: Image[];
		selectedId: string | undefined;
		onselect: (variant: Image) => void;
	} = $props();

	const MAX_TAGS = 3;
</script>

<Table.Root>
	<Table.Header>
		<Table.Row>
			<Table.Head class="w-8 pr-0 pl-3"><span class="sr-only">Selected</span></Table.Head>
			<Table.Head>Branch</Table.Head>
			<Table.Head>Arch</Table.Head>
			<Table.Head>Source</Table.Head>
			<Table.Head class="hidden md:table-cell">Tags</Table.Head>
			<Table.Head class="hidden lg:table-cell">Version</Table.Head>
			<Table.Head class="hidden text-right lg:table-cell">Download</Table.Head>
			<Table.Head class="hidden text-right xl:table-cell">Installed</Table.Head>
			<Table.Head class="hidden pr-4 xl:table-cell">Digest</Table.Head>
		</Table.Row>
	</Table.Header>
	<Table.Body>
		{#each variants as variant (variant.id)}
			{@const selected = variant.id === selectedId}
			<Table.Row
				class="cursor-pointer"
				data-state={selected ? 'selected' : undefined}
				aria-selected={selected}
				onclick={() => onselect(variant)}
			>
				<Table.Cell class="w-8 pr-0 pl-3">
					<button
						type="button"
						class={cn(
							'flex rounded-full outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50',
							selected ? 'text-primary' : 'text-muted-foreground'
						)}
						aria-pressed={selected}
						aria-label="Show {variant.ref} from {variant.registryName}"
						onclick={(e) => {
							e.stopPropagation();
							onselect(variant);
						}}
					>
						{#if selected}<CircleDotIcon class="size-4" />{:else}<CircleIcon class="size-4" />{/if}
					</button>
				</Table.Cell>
				<Table.Cell>
					<div class="flex items-center gap-1.5">
						<span class="font-mono text-xs">{variant.branch || '—'}</span>
						<ExtraDataBadge hasExtraData={variant.hasExtraData} />
					</div>
				</Table.Cell>
				<Table.Cell class="font-mono text-xs">{variant.arch || variant.architecture}</Table.Cell>
				<Table.Cell class="max-w-36 sm:max-w-64">
					<div class="flex min-w-0 flex-col">
						<span class="truncate text-sm">{variant.registryName}</span>
						<span class="truncate font-mono text-xs text-muted-foreground" title={variant.repository}>
							{variant.repository}
						</span>
					</div>
				</Table.Cell>
				<Table.Cell class="hidden md:table-cell">
					<div class="flex flex-wrap gap-1">
						{#each variant.tags.slice(0, MAX_TAGS) as tag (tag)}
							<Badge variant="muted" class="font-mono">{tag}</Badge>
						{:else}
							<span class="text-xs text-muted-foreground">untagged</span>
						{/each}
						{#if variant.tags.length > MAX_TAGS}
							<Badge variant="ghost" title={variant.tags.slice(MAX_TAGS).join(', ')}>
								+{variant.tags.length - MAX_TAGS}
							</Badge>
						{/if}
					</div>
				</Table.Cell>
				<Table.Cell class="hidden max-w-32 truncate text-muted-foreground lg:table-cell" title={variant.version}>
					{variant.version || '—'}
				</Table.Cell>
				<Table.Cell class="hidden text-right tabular-nums lg:table-cell">{formatBytes(variant.downloadSize)}</Table.Cell>
				<Table.Cell class="hidden text-right tabular-nums xl:table-cell">{formatBytes(variant.installedSize)}</Table.Cell>
				<Table.Cell class="hidden pr-4 font-mono text-xs text-muted-foreground xl:table-cell" title={variant.digest}>
					{shortDigest(variant.digest)}
				</Table.Cell>
			</Table.Row>
		{/each}
	</Table.Body>
</Table.Root>
