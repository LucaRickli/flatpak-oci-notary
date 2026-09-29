<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as Tabs from '$lib/components/ui/tabs';
	import * as Table from '$lib/components/ui/table';
	import * as Alert from '$lib/components/ui/alert';
	import { Badge } from '$lib/components/ui/badge';
	import CopyButton from './copy-button.svelte';
	import MetadataView from './metadata/metadata-view.svelte';
	import type { GetImageResponse, Image } from '$lib/api';
	import { formatBytes, formatDate } from '$lib/format';
	import { refHref } from '$lib/links';
	import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';

	let { detail }: { detail: GetImageResponse & { image: Image } } = $props();

	const image = $derived(detail.image);
	const labels = $derived(Object.entries(detail.labels).sort(([a], [b]) => a.localeCompare(b)));
	const runtimeLink = $derived(image.runtime ? refHref(image.runtime) : undefined);
	const extensionOfLink = $derived(image.extensionOf ? refHref(image.extensionOf) : undefined);

	let tab = $state('metadata');
</script>

{#snippet row(label: string, value: string, opts: { mono?: boolean; copy?: boolean; href?: string } = {})}
	<div class="grid gap-1 py-2 sm:grid-cols-[10rem_1fr] sm:gap-4">
		<dt class="text-sm text-muted-foreground">{label}</dt>
		<dd class="flex min-w-0 items-center gap-1">
			{#if opts.href && value}
				<a
					href={opts.href}
					class="{opts.mono
						? 'font-mono text-xs break-all'
						: 'text-sm break-words'} underline decoration-muted-foreground/40 underline-offset-4 hover:decoration-foreground"
					>{value}</a
				>
			{:else}
				<span class={opts.mono ? 'font-mono text-xs break-all' : 'text-sm break-words'}>{value || '—'}</span>
			{/if}
			{#if opts.copy && value}<CopyButton {value} label="Copy {label.toLowerCase()}" />{/if}
		</dd>
	</div>
{/snippet}

<div class="flex flex-col gap-4">
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
					{#if image.runtime}
						{@render row('Runtime', image.runtime, { mono: true, href: runtimeLink })}
					{/if}
					{#if image.extensionOf}
						{@render row('Extension of', image.extensionOf, { mono: true, href: extensionOfLink })}
					{/if}
					{#if image.hasExtraData}
						{@render row('Extra data', 'Downloaded from external URLs during installation')}
					{/if}
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
					{@render row('Registry', image.registryName, { href: `/registries/${image.registryId}` })}
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

	<Tabs.Root bind:value={tab}>
		<Tabs.List>
			<Tabs.Trigger value="metadata">Metadata</Tabs.Trigger>
			<Tabs.Trigger value="labels">Labels ({labels.length})</Tabs.Trigger>
		</Tabs.List>
		<Tabs.Content value="metadata" class="pt-2">
			{#if detail.metadata}
				<!-- The metadata comes from upstream images: if interpreting it ever fails, show it raw
				     rather than losing the whole page. -->
				<svelte:boundary>
					<MetadataView metadata={detail.metadata} {image} />
					{#snippet failed()}
						<Alert.Root variant="destructive">
							<CircleAlertIcon />
							<Alert.Title>The metadata could not be displayed</Alert.Title>
							<Alert.Description>It is shown as found in the image.</Alert.Description>
						</Alert.Root>
						<pre
							class="mt-3 max-h-96 overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs whitespace-pre-wrap [overflow-wrap:anywhere]">{detail.metadata}</pre>
					{/snippet}
				</svelte:boundary>
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
</div>
