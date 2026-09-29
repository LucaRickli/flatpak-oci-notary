<script lang="ts">
	import { goto } from '$app/navigation';
	import * as Table from '$lib/components/ui/table';
	import { Badge } from '$lib/components/ui/badge';
	import type { Package } from '$lib/api';
	import { formatDate, formatRelative, packageTitle } from '$lib/format';
	import { packageHref } from '$lib/links';
	import { rowClick } from '$lib/row-link';
	import PackageIcon from './package-icon.svelte';
	import KindBadge from './kind-badge.svelte';
	import ExtraDataBadge from './metadata/extra-data-badge.svelte';

	let {
		packages,
		showRegistry = true
	}: {
		packages: Package[];
		/** Show the registries column (hide when all packages come from one registry). */
		showRegistry?: boolean;
	} = $props();

	const MAX_CHIPS = 4;
</script>

{#snippet chips(values: string[])}
	<div class="flex flex-wrap gap-1">
		{#each values.slice(0, MAX_CHIPS) as value (value)}
			<Badge variant="muted" class="font-mono">{value}</Badge>
		{:else}
			<span class="text-muted-foreground">—</span>
		{/each}
		{#if values.length > MAX_CHIPS}
			<Badge variant="ghost" title={values.slice(MAX_CHIPS).join(', ')}>+{values.length - MAX_CHIPS}</Badge>
		{/if}
	</div>
{/snippet}

<Table.Root>
	<Table.Header>
		<Table.Row class="hover:bg-transparent">
			<Table.Head class="pl-4">Package</Table.Head>
			<Table.Head>Kind</Table.Head>
			<Table.Head class="hidden md:table-cell">Architectures</Table.Head>
			<Table.Head class="hidden md:table-cell">Branches</Table.Head>
			{#if showRegistry}<Table.Head class="hidden lg:table-cell">Registries</Table.Head>{/if}
			<Table.Head class="hidden xl:table-cell">Version</Table.Head>
			<Table.Head class="hidden pr-4 lg:table-cell">Updated</Table.Head>
		</Table.Row>
	</Table.Header>
	<Table.Body>
		{#each packages as pkg (`${pkg.kind}/${pkg.flatpakId}`)}
			{@const href = packageHref(pkg.kind, pkg.flatpakId)}
			{@const title = packageTitle(pkg)}
			<Table.Row class="h-14 cursor-pointer" onclick={rowClick(() => goto(href))}>
				<Table.Cell class="max-w-56 pl-4 sm:max-w-80">
					<div class="flex items-center gap-3">
						<PackageIcon iconId={pkg.iconImageId} alt={title} />
						<div class="flex min-w-0 flex-col">
							<div class="flex min-w-0 items-center gap-1.5">
								<a {href} class="truncate font-medium hover:underline" title={pkg.flatpakId}>{title}</a>
								<ExtraDataBadge hasExtraData={pkg.hasExtraData} />
							</div>
							<span class="truncate text-xs text-muted-foreground" title={pkg.summary || pkg.flatpakId}>
								{pkg.summary || pkg.flatpakId}
							</span>
						</div>
					</div>
				</Table.Cell>
				<Table.Cell><KindBadge kind={pkg.kind} /></Table.Cell>
				<Table.Cell class="hidden md:table-cell">{@render chips(pkg.architectures)}</Table.Cell>
				<Table.Cell class="hidden md:table-cell">{@render chips(pkg.branches)}</Table.Cell>
				{#if showRegistry}
					<Table.Cell class="hidden max-w-48 lg:table-cell">
						<span class="line-clamp-2 text-sm whitespace-normal">
							{pkg.registries.map((r) => r.name).join(', ') || '—'}
						</span>
					</Table.Cell>
				{/if}
				<Table.Cell class="hidden max-w-32 truncate text-muted-foreground xl:table-cell" title={pkg.version}>
					{pkg.version || '—'}
				</Table.Cell>
				<Table.Cell class="hidden pr-4 text-muted-foreground lg:table-cell">
					<span title={formatDate(pkg.updated)}>{pkg.updated ? formatRelative(pkg.updated) : '—'}</span>
				</Table.Cell>
			</Table.Row>
		{/each}
	</Table.Body>
</Table.Root>
