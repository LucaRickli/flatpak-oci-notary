<script lang="ts">
	import { goto } from '$app/navigation';
	import * as Table from '$lib/components/ui/table';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import { Badge } from '$lib/components/ui/badge';
	import type { Image } from '$lib/api';
	import { formatBytes, imageTitle } from '$lib/format';
	import { imageHref } from '$lib/links';
	import { rowClick } from '$lib/row-link';
	import PackageIcon from './package-icon.svelte';
	import KindBadge from './kind-badge.svelte';
	import ExtraDataBadge from './metadata/extra-data-badge.svelte';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

	let {
		images,
		showRegistry = true,
		missingRuntimes
	}: {
		images: Image[];
		/** Show the registry column (hide when all images come from one registry). */
		showRegistry?: boolean;
		/**
		 * Runtime refs clients can't get, each with the flatpak IDs that need it to install
		 * (MissingRuntime.neededBy); rows of those images get a warning. An image whose runtime is
		 * informational (an SDK, an extension without extra data) is not flagged.
		 */
		missingRuntimes?: ReadonlyMap<string, ReadonlySet<string>>;
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
			{@const href = imageHref(image)}
			{@const runtimeMissing = !!image.runtime && !!missingRuntimes?.get(image.runtime)?.has(image.flatpakId)}
			<Table.Row class="cursor-pointer" onclick={rowClick(() => goto(href))}>
				<Table.Cell class="max-w-56 sm:max-w-80">
					<div class="flex items-center gap-3">
						<PackageIcon {image} />
						<div class="flex min-w-0 flex-col">
							<div class="flex min-w-0 items-center gap-1.5">
								<a {href} class="truncate font-medium hover:underline">{imageTitle(image)}</a>
								{#if runtimeMissing}
									<Tooltip.Root>
										<Tooltip.Trigger>
											{#snippet child({ props })}
												<span
													{...props}
													class="inline-flex shrink-0 text-amber-600 dark:text-amber-500"
													aria-label="Runtime not served"
												>
													<TriangleAlertIcon class="size-3.5" />
												</span>
											{/snippet}
										</Tooltip.Trigger>
										<Tooltip.Content class="max-w-xs">
											Needs <span class="font-mono">{image.runtime}</span>, which this repository doesn't serve.
										</Tooltip.Content>
									</Tooltip.Root>
								{/if}
								<ExtraDataBadge hasExtraData={image.hasExtraData} />
							</div>
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
