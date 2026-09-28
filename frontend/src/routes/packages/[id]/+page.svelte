<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as Tabs from '$lib/components/ui/tabs';
	import * as Table from '$lib/components/ui/table';
	import * as Empty from '$lib/components/ui/empty';
	import * as Item from '$lib/components/ui/item';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import PageHeader from '$lib/components/page-header.svelte';
	import PackageIcon from '$lib/components/package-icon.svelte';
	import KindBadge from '$lib/components/kind-badge.svelte';
	import CopyButton from '$lib/components/copy-button.svelte';
	import CodeBlock from '$lib/components/code-block.svelte';
	import ErrorAlert from '$lib/components/error-alert.svelte';
	import { imageClient } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { isNotFound } from '$lib/session.svelte';
	import { setCrumb } from '$lib/breadcrumb.svelte';
	import { formatBytes, formatDate, imageTitle } from '$lib/format';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';

	let { data } = $props();

	const detail = new Resource(() => imageClient.getImage({ id: data.id }));
	const current = $derived(detail.current?.image?.id === data.id ? detail.current : undefined);
	const image = $derived(current?.image);

	// Other images of the same flatpak (different arches, branches or tags).
	const variants = new Resource(async () => {
		const flatpakId = image?.flatpakId;
		if (!flatpakId) return [];
		const { images } = await imageClient.listImages({ query: flatpakId });
		return images.filter((i) => i.flatpakId === flatpakId);
	});
	const otherVariants = $derived((variants.current ?? []).filter((i) => i.id !== data.id));

	const labels = $derived(
		Object.entries(current?.labels ?? {}).sort(([a], [b]) => a.localeCompare(b))
	);

	$effect(() => {
		if (image) setCrumb(imageTitle(image));
	});
</script>

{#snippet row(label: string, value: string, opts: { mono?: boolean; copy?: boolean } = {})}
	<div class="grid gap-1 py-2 sm:grid-cols-[10rem_1fr] sm:gap-4">
		<dt class="text-sm text-muted-foreground">{label}</dt>
		<dd class="flex min-w-0 items-center gap-1">
			<span class={opts.mono ? 'font-mono text-xs break-all' : 'text-sm break-words'}>{value || '—'}</span>
			{#if opts.copy && value}<CopyButton {value} label="Copy {label.toLowerCase()}" />{/if}
		</dd>
	</div>
{/snippet}

{#if detail.error}
	{#if isNotFound(detail.error)}
		<Empty.Root>
			<Empty.Header>
				<Empty.Title>Package not found</Empty.Title>
				<Empty.Description>It may have been removed from its registry during a sync.</Empty.Description>
			</Empty.Header>
			<Empty.Content><Button href="/packages" variant="outline">Back to packages</Button></Empty.Content>
		</Empty.Root>
	{:else}
		<ErrorAlert error={detail.error} onretry={detail.refresh} />
	{/if}
{:else if !current || !image}
	<div class="flex items-center gap-4">
		<Skeleton class="size-16 rounded-xl" />
		<div class="flex flex-col gap-2">
			<Skeleton class="h-7 w-56" />
			<Skeleton class="h-4 w-80" />
		</div>
	</div>
	<Skeleton class="h-64 rounded-xl" />
{:else}
	<PageHeader title={imageTitle(image)} description={image.summary}>
		{#snippet media()}
			<PackageIcon {image} class="size-16 rounded-xl" />
		{/snippet}
		<div class="flex flex-wrap items-center gap-1.5 pt-1">
			<KindBadge kind={image.kind} />
			<Badge variant="outline" class="font-mono">{image.flatpakId}</Badge>
			<Badge variant="outline" class="font-mono">{image.arch || image.architecture}</Badge>
			<Badge variant="outline" class="font-mono">{image.branch}</Badge>
			{#if image.version}<Badge variant="secondary">v{image.version}</Badge>{/if}
		</div>
	</PageHeader>

	<div class="grid gap-4 lg:grid-cols-2">
		<Card.Root>
			<Card.Header>
				<Card.Title>Flatpak</Card.Title>
				<Card.Description>What flatpak sees when installing this image.</Card.Description>
			</Card.Header>
			<Card.Content>
				<dl class="flex flex-col divide-y">
					{@render row('Ref', image.ref, { mono: true, copy: true })}
					{@render row('Version', image.version)}
					{@render row('Download size', formatBytes(image.downloadSize))}
					{@render row('Installed size', formatBytes(image.installedSize))}
					{@render row('Created', formatDate(image.created))}
				</dl>
			</Card.Content>
		</Card.Root>
		<Card.Root>
			<Card.Header>
				<Card.Title>OCI image</Card.Title>
				<Card.Description>Where the image lives upstream.</Card.Description>
			</Card.Header>
			<Card.Content>
				<dl class="flex flex-col divide-y">
					<div class="grid gap-1 py-2 sm:grid-cols-[10rem_1fr] sm:gap-4">
						<dt class="text-sm text-muted-foreground">Registry</dt>
						<dd><a href="/registries/{image.registryId}" class="text-sm hover:underline">{image.registryName}</a></dd>
					</div>
					{@render row('Repository', image.repository, { mono: true, copy: true })}
					<div class="grid gap-1 py-2 sm:grid-cols-[10rem_1fr] sm:gap-4">
						<dt class="text-sm text-muted-foreground">Tags</dt>
						<dd class="flex flex-wrap gap-1">
							{#each image.tags as tag (tag)}
								<Badge variant="outline" class="font-mono">{tag}</Badge>
							{:else}
								<span class="text-sm">—</span>
							{/each}
						</dd>
					</div>
					{@render row('Digest', image.digest, { mono: true, copy: true })}
					{@render row('Platform', `${image.os}/${image.architecture}`, { mono: true })}
					{@render row('Media type', image.mediaType, { mono: true })}
					{@render row('Indexed', formatDate(image.indexedAt))}
				</dl>
			</Card.Content>
		</Card.Root>
	</div>

	{#if otherVariants.length > 0}
		<Card.Root>
			<Card.Header>
				<Card.Title>Other variants</Card.Title>
				<Card.Description>Images of {image.flatpakId} for other architectures, branches or tags.</Card.Description>
			</Card.Header>
			<Card.Content class="flex flex-col gap-2">
				{#each otherVariants as variant (variant.id)}
					<Item.Root variant="outline" size="sm">
						{#snippet child({ props })}
							<a href="/packages/{variant.id}" {...props}>
								<Item.Content>
									<Item.Title class="font-mono text-xs">{variant.ref}</Item.Title>
									<Item.Description class="font-mono text-xs">
										{variant.registryName} · {variant.repository} · {variant.tags.join(', ') || 'untagged'}
									</Item.Description>
								</Item.Content>
								<Item.Actions><ChevronRightIcon class="size-4" /></Item.Actions>
							</a>
						{/snippet}
					</Item.Root>
				{/each}
			</Card.Content>
		</Card.Root>
	{/if}

	<Tabs.Root value="metadata">
		<Tabs.List>
			<Tabs.Trigger value="metadata">Metadata</Tabs.Trigger>
			<Tabs.Trigger value="labels">Labels ({labels.length})</Tabs.Trigger>
		</Tabs.List>
		<Tabs.Content value="metadata" class="pt-2">
			{#if current.metadata}
				<CodeBlock code={current.metadata} />
			{:else}
				<p class="text-sm text-muted-foreground">No org.flatpak.metadata label on this image.</p>
			{/if}
		</Tabs.Content>
		<Tabs.Content value="labels" class="pt-2">
			<Card.Root class="py-0">
				<Table.Root class="table-fixed">
					<Table.Header>
						<Table.Row>
							<Table.Head class="w-1/3 pl-4">Label</Table.Head>
							<Table.Head class="pr-4">Value</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#each labels as [key, value] (key)}
							<Table.Row>
								<Table.Cell class="pl-4 align-top font-mono text-xs break-all whitespace-normal">{key}</Table.Cell>
								<Table.Cell class="pr-4 font-mono text-xs break-all whitespace-pre-wrap">{value}</Table.Cell>
							</Table.Row>
						{:else}
							<Table.Row>
								<Table.Cell colspan={2} class="text-center text-muted-foreground">No labels</Table.Cell>
							</Table.Row>
						{/each}
					</Table.Body>
				</Table.Root>
			</Card.Root>
		</Tabs.Content>
	</Tabs.Root>
{/if}
