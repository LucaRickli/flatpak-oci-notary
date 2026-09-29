<script lang="ts">
	// Parsed view of an image's flatpak metadata (the org.flatpak.metadata
	// label): install summary, permissions, app/runtime info and the raw file.
	import * as Alert from '$lib/components/ui/alert';
	import * as Card from '$lib/components/ui/card';
	import * as Empty from '$lib/components/ui/empty';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import CodeBlock from '$lib/components/code-block.svelte';
	import CopyButton from '$lib/components/copy-button.svelte';
	import { RefKind, type Image } from '$lib/api';
	import { describeRequiredFlatpak, parseMetadata, type FlatpakKind } from '$lib/flatpak/metadata';
	import InstallSummary from './install-summary.svelte';
	import PermissionsCard from './permissions-card.svelte';
	import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import ChevronUpIcon from '@lucide/svelte/icons/chevron-up';
	import FileCodeIcon from '@lucide/svelte/icons/file-code';

	let {
		metadata,
		image,
		raw = true
	}: {
		metadata: string;
		image: Image;
		/** Include the collapsible raw metadata (off when the page shows it elsewhere). */
		raw?: boolean;
	} = $props();

	const kind = $derived<FlatpakKind | undefined>(
		image.kind === RefKind.APP ? 'app' : image.kind === RefKind.RUNTIME ? 'runtime' : undefined
	);
	const empty = $derived(metadata.trim() === '');
	const result = $derived(empty ? undefined : parseMetadata(metadata, { kind }));
	const meta = $derived(result?.ok ? result.metadata : undefined);
	const required = $derived(describeRequiredFlatpak(meta?.requiredFlatpak ?? []));
	const title = $derived(
		meta?.extensionOf || image.extensionOf ? 'Extension' : meta?.kind === 'app' ? 'Application' : 'Runtime'
	);

	// Raw metadata starts expanded when it could not be interpreted.
	let rawOverride = $state<boolean>();
	const showRaw = $derived(rawOverride ?? !meta);

	const MAX_ERRORS = 5;
</script>

{#snippet row(label: string, value: string | undefined, opts: { mono?: boolean; copy?: boolean } = {})}
	<div class="grid gap-1 py-2 sm:grid-cols-[9rem_1fr] sm:gap-4">
		<dt class="text-sm text-muted-foreground">{label}</dt>
		<dd class="flex min-w-0 items-center gap-1">
			<span class={opts.mono ? 'font-mono text-xs break-all' : 'text-sm break-words'}>{value || '—'}</span>
			{#if opts.copy && value}<CopyButton {value} label="Copy {label.toLowerCase()}" />{/if}
		</dd>
	</div>
{/snippet}

<div class="flex flex-col gap-4">
	{#if empty}
		<Empty.Root class="border border-dashed">
			<Empty.Header>
				<Empty.Media variant="icon"><FileCodeIcon /></Empty.Media>
				<Empty.Title>No flatpak metadata</Empty.Title>
				<Empty.Description>
					The image has no org.flatpak.metadata label, so flatpak cannot install it.
				</Empty.Description>
			</Empty.Header>
		</Empty.Root>
	{:else if result && !result.ok}
		<Alert.Root variant="destructive">
			<CircleAlertIcon />
			<Alert.Title>The metadata is not a valid key file</Alert.Title>
			<Alert.Description>
				<p>Flatpak refuses to load it, so the image cannot be installed.</p>
				<ul class="flex flex-col gap-0.5 font-mono text-xs">
					{#each result.errors.slice(0, MAX_ERRORS) as error (error.line)}
						<li>Line {error.line}: {error.message}</li>
					{/each}
					{#if result.errors.length > MAX_ERRORS}
						<li>… and {result.errors.length - MAX_ERRORS} more</li>
					{/if}
				</ul>
			</Alert.Description>
		</Alert.Root>
	{:else if meta}
		<div class="grid gap-4 lg:grid-cols-2 lg:items-start">
			<PermissionsCard {meta} />
			<div class="flex flex-col gap-4">
				<InstallSummary {meta} {image} />
				<Card.Root>
					<Card.Header>
						<Card.Title>{title}</Card.Title>
						<Card.Description>[{meta.mainGroup ?? 'Application'}] group</Card.Description>
					</Card.Header>
					<Card.Content>
						{#if !meta.mainGroup}
							<p class="text-sm text-destructive">
								Neither an [Application] nor a [Runtime] group is present; flatpak cannot use this metadata.
							</p>
						{:else}
							<dl class="flex flex-col divide-y">
								{@render row('ID', meta.name, { mono: true, copy: true })}
								{#if meta.kind === 'app'}
									{@render row('Command', meta.command, { mono: true })}
								{/if}
								<div class="grid gap-1 py-2 sm:grid-cols-[9rem_1fr] sm:gap-4">
									<dt class="text-sm text-muted-foreground">Requires flatpak</dt>
									<dd class="flex flex-col gap-0.5 text-sm">
										{#if required.minimum}
											<span>{required.minimum} or newer</span>
											{#if required.backports.length > 0}
												<span class="text-xs text-muted-foreground">
													Also {required.backports.map((v) => `${v}+`).join(', ')} in older release series
												</span>
											{/if}
										{:else if required.invalid.length === 0}
											<span class="text-muted-foreground">Any version</span>
										{/if}
										{#if required.invalid.length > 0}
											<span class="text-xs text-destructive">
												Invalid: {required.invalid.join(', ')} (flatpak refuses to install)
											</span>
										{/if}
									</dd>
								</div>
								{#if meta.tags.length > 0}
									<div class="grid gap-1 py-2 sm:grid-cols-[9rem_1fr] sm:gap-4">
										<dt class="text-sm text-muted-foreground">Tags</dt>
										<dd class="flex flex-wrap gap-1">
											{#each meta.tags as tag, i (i)}
												<Badge variant="outline">{tag}</Badge>
											{/each}
										</dd>
									</div>
								{/if}
								{#if meta.unknownGroups.length > 0}
									<div class="grid gap-1 py-2 sm:grid-cols-[9rem_1fr] sm:gap-4">
										<dt class="text-sm text-muted-foreground">Other groups</dt>
										<dd class="flex flex-wrap gap-1">
											{#each meta.unknownGroups as group (group)}
												<Badge variant="outline" class="font-mono">[{group}]</Badge>
											{/each}
										</dd>
									</div>
								{/if}
							</dl>
						{/if}
					</Card.Content>
				</Card.Root>
			</div>
		</div>
	{/if}

	{#if !empty && raw}
		<Card.Root size="sm">
			<Card.Header>
				<Card.Title>Raw metadata</Card.Title>
				<Card.Description>The org.flatpak.metadata label as stored in the image.</Card.Description>
				<Card.Action>
					<Button
						variant="outline"
						size="sm"
						aria-expanded={showRaw}
						onclick={() => (rawOverride = !showRaw)}
					>
						{#if showRaw}
							<ChevronUpIcon data-icon="inline-start" />Hide
						{:else}
							<ChevronDownIcon data-icon="inline-start" />Show
						{/if}
					</Button>
				</Card.Action>
			</Card.Header>
			{#if showRaw}
				<Card.Content>
					<CodeBlock code={metadata} />
				</Card.Content>
			{/if}
		</Card.Root>
	{/if}
</div>
