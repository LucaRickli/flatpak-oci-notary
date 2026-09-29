<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import { RefKind, type Package } from '$lib/api';
	import { packageTitle } from '$lib/format';
	import { packageHref } from '$lib/links';
	import PackageIcon from './package-icon.svelte';
	import KindBadge from './kind-badge.svelte';
	import ExtraDataBadge from './metadata/extra-data-badge.svelte';
	import GitBranchIcon from '@lucide/svelte/icons/git-branch';
	import ServerIcon from '@lucide/svelte/icons/server';

	let { pkg, showRegistry = true }: { pkg: Package; showRegistry?: boolean } = $props();

	const title = $derived(packageTitle(pkg));
	const href = $derived(packageHref(pkg.kind, pkg.flatpakId));
	const MAX_ARCHES = 2;
</script>

<Card.Root
	class="relative gap-3 transition-[box-shadow,background-color] hover:bg-accent/40 hover:shadow-md hover:ring-primary/30 has-[a:focus-visible]:ring-2 has-[a:focus-visible]:ring-ring"
>
	<Card.Content class="flex items-start gap-4">
		<PackageIcon iconId={pkg.iconImageId} alt="" class="size-14 rounded-xl" />
		<div class="flex min-w-0 flex-1 flex-col gap-0.5 pt-0.5">
			<h3 class="truncate text-base font-semibold">
				<!-- The link covers the whole card; chips with tooltips sit above it. -->
				<a {href} class="outline-none after:absolute after:inset-0" title={pkg.flatpakId}>{title}</a>
			</h3>
			<p class="line-clamp-2 min-h-10 text-sm text-muted-foreground">{pkg.summary || pkg.flatpakId}</p>
		</div>
	</Card.Content>
	<Card.Footer class="mt-auto flex-wrap gap-1.5 py-2.5 text-xs text-muted-foreground">
		{#if pkg.kind !== RefKind.APP}<KindBadge kind={pkg.kind} />{/if}
		{#if pkg.hasExtraData}<span class="relative flex"><ExtraDataBadge hasExtraData compact /></span>{/if}
		<span class="font-mono" title={pkg.architectures.join(', ')}>
			{pkg.architectures.slice(0, MAX_ARCHES).join(' · ')}{pkg.architectures.length > MAX_ARCHES
				? ` +${pkg.architectures.length - MAX_ARCHES}`
				: ''}
		</span>
		<span class="ml-auto flex items-center gap-2.5">
			{#if pkg.branches.length > 1}
				<span class="flex items-center gap-1" title={pkg.branches.join(', ')}>
					<GitBranchIcon class="size-3.5" />{pkg.branches.length}
				</span>
			{:else if pkg.branches[0]}
				<span class="flex items-center gap-1 font-mono">
					<GitBranchIcon class="size-3.5" />{pkg.branches[0]}
				</span>
			{/if}
			{#if showRegistry && pkg.registries.length > 0}
				<Tooltip.Root>
					<Tooltip.Trigger>
						{#snippet child({ props })}
							<span {...props} class="relative flex items-center gap-1">
								<ServerIcon class="size-3.5" />{pkg.registries.length > 1 ? pkg.registries.length : pkg.registries[0].name}
							</span>
						{/snippet}
					</Tooltip.Trigger>
					<Tooltip.Content>{pkg.registries.map((r) => r.name).join(', ')}</Tooltip.Content>
				</Tooltip.Root>
			{/if}
		</span>
	</Card.Footer>
</Card.Root>
