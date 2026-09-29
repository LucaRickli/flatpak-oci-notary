<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as Item from '$lib/components/ui/item';
	import { Badge } from '$lib/components/ui/badge';
	import { Separator } from '$lib/components/ui/separator';
	import { RefKind, type Image } from '$lib/api';
	import {
		describeExtensionCondition,
		parseFullRef,
		parsePartialRef,
		runtimeDependency,
		type ExtensionPoint,
		type FlatpakMetadata
	} from '$lib/flatpak/metadata';
	import ExtraDataAlert from './extra-data-alert.svelte';
	import RuntimeAvailability from './runtime-availability.svelte';
	import { packageHref } from '$lib/links';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import PuzzleIcon from '@lucide/svelte/icons/puzzle';

	let { meta, image }: { meta: FlatpakMetadata; image: Image } = $props();

	const kindWord = $derived(meta.extensionOf || image.extensionOf ? 'extension' : meta.kind === 'app' ? 'app' : 'runtime');
	const dependency = $derived(runtimeDependency(meta, image.runtime));
	const sdk = $derived(meta.sdk ? parsePartialRef(meta.sdk) : undefined);
	const extendsRef = $derived(meta.extensionOf?.ref || image.extensionOf);
	const extendsParsed = $derived(extendsRef ? parseFullRef(extendsRef) : undefined);
	/** Extension directories are relative to /app for apps, /usr for runtimes. */
	const mountRoot = $derived(meta.kind === 'app' ? '/app' : '/usr');
	const autoCount = $derived(meta.extensions.filter((e) => e.download === 'auto').length);

	const DOWNLOAD_LABEL: Record<ExtensionPoint['download'], string> = {
		auto: 'Downloaded automatically',
		conditional: 'Conditional download',
		manual: 'Optional',
		debug: 'Debug info, optional'
	};

	function joinConditions(conditions: string[]): string {
		return conditions.map(describeExtensionCondition).join(' or ');
	}
</script>

{#snippet extension(ext: ExtensionPoint)}
	<Item.Root variant="outline" size="sm">
		<Item.Content class="min-w-0">
			<Item.Title class="line-clamp-none flex-wrap">
				<span class="font-mono text-xs break-all">{ext.name}{ext.subdirectories ? '.*' : ''}</span>
				{#if ext.tag}<Badge variant="outline">@{ext.tag}</Badge>{/if}
			</Item.Title>
			<ul class="flex flex-col gap-0.5 text-xs text-muted-foreground">
				{#if ext.download === 'conditional'}
					<li>Downloaded only if {joinConditions(ext.downloadIf)}.</li>
				{:else if ext.download === 'manual'}
					<li>Not downloaded automatically; install it separately.</li>
				{:else if ext.download === 'debug'}
					<li>Debug symbols are never downloaded automatically.</li>
				{/if}
				{#if ext.partial}<li>Only the configured languages are downloaded.</li>{/if}
				{#if ext.enableIf.length > 0}<li>Used only if {joinConditions(ext.enableIf)}.</li>{/if}
				<li>
					{#if !ext.directory}
						No directory set: flatpak ignores this extension point.
					{:else if ext.subdirectories}
						Each <code class="font-mono">{ext.name}.*</code> extension is mounted in its own subdirectory of
						<code class="font-mono">{mountRoot}/{ext.directory}</code>{ext.subdirectorySuffix
							? ` (…/${ext.subdirectorySuffix})`
							: ''}.
					{:else}
						Mounted at <code class="font-mono">{mountRoot}/{ext.directory}</code>.
					{/if}
				</li>
				<li>
					Branch: {ext.versions ? ext.versions.join(', ') || '(none)' : `same as this ${kindWord}${image.branch ? ` (${image.branch})` : ''}`}
				</li>
				{#if ext.removedWithParent}<li>Removed together with this {kindWord}.</li>{/if}
			</ul>
		</Item.Content>
		<Item.Actions class="self-start">
			<Badge variant={ext.download === 'auto' ? 'secondary' : 'outline'}>
				{#if ext.download === 'auto'}<DownloadIcon data-icon="inline-start" />{/if}
				{DOWNLOAD_LABEL[ext.download]}
			</Badge>
		</Item.Actions>
	</Item.Root>
{/snippet}

<Card.Root>
	<Card.Header>
		<Card.Title>Install summary</Card.Title>
		<Card.Description>What installing this {kindWord} pulls in.</Card.Description>
	</Card.Header>
	<Card.Content class="flex flex-col gap-4">
		{#if meta.extraData}
			<ExtraDataAlert extraData={meta.extraData} />
		{/if}

		{#if extendsRef}
			<div class="flex flex-col gap-1">
				<span class="flex items-center gap-2 text-sm font-medium">
					<PuzzleIcon class="size-4 text-muted-foreground" />Extension of
				</span>
				{#if extendsParsed}
					<a
						href={packageHref(extendsParsed.kind === 'app' ? RefKind.APP : RefKind.RUNTIME, extendsParsed.id)}
						class="w-fit font-mono text-xs break-all hover:underline">{extendsRef}</a
					>
				{:else}
					<span class="font-mono text-xs break-all">{extendsRef}</span>
				{/if}
				<p class="text-xs text-muted-foreground">
					Mounted into it at its matching extension point{meta.extensionOf?.tag
						? ` (tag “${meta.extensionOf.tag}”)`
						: ''}{meta.extensionOf?.priority ? `, priority ${meta.extensionOf.priority}` : ''}.
				</p>
			</div>
		{/if}

		{#if dependency}
			<RuntimeAvailability {dependency} arch={image.arch} />
		{:else if meta.kind === 'runtime'}
			<p class="text-sm text-muted-foreground">No runtime dependency: this is a base runtime.</p>
		{/if}

		{#if meta.sdk}
			<div class="flex flex-col gap-1">
				<div class="flex flex-wrap items-center gap-2">
					<span class="text-sm font-medium">SDK</span>
					<Badge variant="outline">Development only</Badge>
				</div>
				{#if sdk}
					<a href={packageHref(RefKind.RUNTIME, sdk.id)} class="w-fit font-mono text-xs break-all hover:underline">{meta.sdk}</a>
				{:else}
					<span class="font-mono text-xs break-all">{meta.sdk}</span>
				{/if}
				<p class="text-xs text-muted-foreground">Used to build it; not installed for users.</p>
			</div>
		{/if}

		{#if meta.extensions.length > 0}
			<Separator />
			<div class="flex flex-col gap-2">
				<div class="flex flex-wrap items-baseline justify-between gap-2">
					<span class="text-sm font-medium">Extension points ({meta.extensions.length})</span>
					<span class="text-xs text-muted-foreground">
						{autoCount === 0 ? 'None downloaded automatically' : `${autoCount} downloaded automatically`}
					</span>
				</div>
				<p class="text-xs text-muted-foreground">
					Extensions are looked up in the remote this {kindWord} is installed from, on install and update.
				</p>
				{#each meta.extensions as ext (ext.group)}
					{@render extension(ext)}
				{/each}
			</div>
		{/if}
	</Card.Content>
</Card.Root>
