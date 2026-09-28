<script lang="ts">
	import { goto } from '$app/navigation';
	import * as Table from '$lib/components/ui/table';
	import { Badge } from '$lib/components/ui/badge';
	import type { Image } from '$lib/api';
	import { formatBytes, imageTitle } from '$lib/format';
	import PackageIcon from './package-icon.svelte';
	import KindBadge from './kind-badge.svelte';

	let {
		images,
		showRegistry = true
	}: {
		images: Image[];
		/** Show the registry column (hide when all images come from one registry). */
		showRegistry?: boolean;
	} = $props();

	const MAX_TAGS = 3;
</script>

<Table.Root>
	<Table.Header>
		<Table.Row>
			<Table.Head>Package</Table.Head>
			<Table.Head>Kind</Table.Head>
			<Table.Head class="hidden md:table-cell">Arch</Table.Head>
			<Table.Head class="hidden md:table-cell">Branch</Table.Head>
			<Table.Head class="hidden lg:table-cell">Tags</Table.Head>
			<Table.Head class="hidden xl:table-cell">Version</Table.Head>
			<Table.Head class="hidden text-right lg:table-cell">Download</Table.Head>
			<Table.Head class="hidden text-right xl:table-cell">Installed</Table.Head>
			<Table.Head class="hidden 2xl:table-cell">{showRegistry ? 'Source' : 'Repository'}</Table.Head>
		</Table.Row>
	</Table.Header>
	<Table.Body>
		{#each images as image (image.id)}
			<Table.Row class="cursor-pointer" onclick={() => goto(`/packages/${image.id}`)}>
				<Table.Cell class="max-w-56 sm:max-w-80">
					<div class="flex items-center gap-3">
						<PackageIcon {image} />
						<div class="flex min-w-0 flex-col">
							<a
								href="/packages/{image.id}"
								class="truncate font-medium hover:underline"
								onclick={(e) => e.stopPropagation()}>{imageTitle(image)}</a
							>
							<span class="truncate text-xs text-muted-foreground" title={image.ref}>
								{image.summary || image.ref}
							</span>
						</div>
					</div>
				</Table.Cell>
				<Table.Cell><KindBadge kind={image.kind} /></Table.Cell>
				<Table.Cell class="hidden font-mono text-xs md:table-cell">{image.arch || image.architecture}</Table.Cell>
				<Table.Cell class="hidden font-mono text-xs md:table-cell">{image.branch}</Table.Cell>
				<Table.Cell class="hidden lg:table-cell">
					<div class="flex flex-wrap gap-1">
						{#each image.tags.slice(0, MAX_TAGS) as tag (tag)}
							<Badge variant="outline" class="font-mono">{tag}</Badge>
						{/each}
						{#if image.tags.length > MAX_TAGS}
							<Badge variant="ghost" title={image.tags.slice(MAX_TAGS).join(', ')}>
								+{image.tags.length - MAX_TAGS}
							</Badge>
						{/if}
					</div>
				</Table.Cell>
				<Table.Cell class="hidden text-muted-foreground xl:table-cell">{image.version || '—'}</Table.Cell>
				<Table.Cell class="hidden text-right tabular-nums lg:table-cell">
					{formatBytes(image.downloadSize)}
				</Table.Cell>
				<Table.Cell class="hidden text-right tabular-nums xl:table-cell">
					{formatBytes(image.installedSize)}
				</Table.Cell>
				<Table.Cell class="hidden max-w-64 2xl:table-cell">
					<div class="flex min-w-0 flex-col">
						{#if showRegistry}<span class="truncate text-sm">{image.registryName}</span>{/if}
						<span class="truncate font-mono text-xs text-muted-foreground">{image.repository}</span>
					</div>
				</Table.Cell>
			</Table.Row>
		{/each}
	</Table.Body>
</Table.Root>
