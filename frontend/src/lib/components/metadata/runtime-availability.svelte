<script lang="ts">
	import * as Alert from '$lib/components/ui/alert';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { RefKind, errorMessage, imageClient, type Image } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { isNotFound } from '$lib/session.svelte';
	import type { RuntimeDependency } from '$lib/flatpak/metadata';
	import { packageHref } from '$lib/links';
	import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
	import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';

	let {
		dependency,
		arch = ''
	}: {
		dependency: RuntimeDependency;
		/** Flatpak arch to match when the runtime ref has none (the image's arch). */
		arch?: string;
	} = $props();

	const id = $derived(dependency.ref?.id ?? '');
	const wantArch = $derived(dependency.ref?.arch || arch);
	const wantBranch = $derived(dependency.ref?.branch ?? '');
	const required = $derived(dependency.role !== 'base');

	const lookup = new Resource(async () => {
		const flatpakId = id;
		if (!flatpakId) return undefined;
		return imageClient.getPackage({ kind: RefKind.RUNTIME, flatpakId });
	});
	// Ignore a stale result while another runtime loads.
	const found = $derived(lookup.current?.package?.flatpakId === id ? lookup.current : undefined);
	const notFound = $derived(!!lookup.error && isNotFound(lookup.error));

	const matches = (v: Image) =>
		(!wantArch || v.arch === wantArch) && (!wantBranch || v.branch === wantBranch);
	const matching = $derived((found?.variants ?? []).filter(matches));

	/** Registries with a matching variant, each with the repositories providing it. */
	const providers = $derived.by(() => {
		const byId = new Map<string, { id: string; name: string; repositories: Set<string> }>();
		for (const v of matching) {
			let p = byId.get(v.registryId);
			if (!p) byId.set(v.registryId, (p = { id: v.registryId, name: v.registryName, repositories: new Set() }));
			p.repositories.add(v.repository);
		}
		return [...byId.values()].sort((a, b) => a.name.localeCompare(b.name));
	});

	/** What is indexed instead, when nothing matches: branches for this arch, else other arches. */
	const indexedInstead = $derived.by(() => {
		const variants = found?.variants ?? [];
		const sameArch = [...new Set(variants.filter((v) => !wantArch || v.arch === wantArch).map((v) => v.branch))];
		if (sameArch.length > 0) {
			return `for ${wantArch || 'this architecture'} only with branch ${sameArch.sort().join(', ')}`;
		}
		const arches = [...new Set(variants.map((v) => v.arch))].sort();
		return `only for ${arches.join(', ') || 'other architectures'}`;
	});

	const roleLabel = $derived(
		{ required: 'Required', 'extra-data': 'Needed to install', base: 'Built against' }[dependency.role]
	);
	const roleHint = $derived(
		{
			required: 'Installed with the app from any configured remote that provides it.',
			'extra-data': 'The extra data install script runs inside this runtime, so it must be installed.',
			base: 'Informational: not installed as a dependency.'
		}[dependency.role]
	);
	const wanted = $derived([wantArch, wantBranch].filter(Boolean).join('/'));
</script>

<div class="flex flex-col gap-2">
	<div class="flex flex-wrap items-center gap-2">
		<span class="text-sm font-medium">Runtime</span>
		<Badge variant={required ? 'secondary' : 'outline'}>{roleLabel}</Badge>
	</div>
	{#if id}
		<a href={packageHref(RefKind.RUNTIME, id)} class="w-fit font-mono text-xs break-all hover:underline">
			{dependency.pref}
		</a>
	{:else}
		<span class="font-mono text-xs break-all">{dependency.pref}</span>
		<span class="text-xs text-destructive">Not a valid runtime reference (expected id/arch/branch).</span>
	{/if}
	<p class="text-xs text-muted-foreground">{roleHint}</p>

	{#if id}
		{#if notFound}
			{#if required}
				<Alert.Root variant="destructive">
					<CircleAlertIcon />
					<Alert.Title>Not indexed in any registry</Alert.Title>
					<Alert.Description>
						Installing fails unless another remote configured on the client provides runtime/{dependency.pref}.
					</Alert.Description>
				</Alert.Root>
			{:else}
				<p class="text-xs text-muted-foreground">Not indexed in any registry.</p>
			{/if}
		{:else if lookup.error}
			<div class="flex flex-wrap items-center gap-2 text-xs text-destructive">
				<CircleAlertIcon class="size-3.5" />
				Could not check availability: {errorMessage(lookup.error)}
				<Button variant="ghost" size="xs" onclick={lookup.refresh}>
					<RefreshCwIcon data-icon="inline-start" />Retry
				</Button>
			</div>
		{:else if !found}
			<Skeleton class="h-5 w-56" />
		{:else if providers.length > 0}
			<div class="flex flex-wrap items-center gap-1.5 text-xs">
				<CircleCheckIcon class="size-3.5 text-muted-foreground" />
				<span class="text-muted-foreground">Indexed{wanted ? ` for ${wanted}` : ''} in</span>
				{#each providers as p (p.id)}
					<Badge variant="outline" href="/registries/{p.id}" title={[...p.repositories].join(', ')}>
						{p.name}
					</Badge>
				{/each}
			</div>
		{:else}
			<Alert.Root variant={required ? 'destructive' : 'default'}>
				<TriangleAlertIcon />
				<Alert.Title>Not indexed for {wanted || 'this architecture and branch'}</Alert.Title>
				<Alert.Description>
					{found.package?.name || id} is indexed {indexedInstead}.{required
						? ' Installing fails unless another configured remote provides the required branch.'
						: ''}
				</Alert.Description>
			</Alert.Root>
		{/if}
	{/if}
</div>
