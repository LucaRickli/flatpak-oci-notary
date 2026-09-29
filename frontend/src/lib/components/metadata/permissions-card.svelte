<script lang="ts">
	import type { Component } from 'svelte';
	import * as Card from '$lib/components/ui/card';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import type { FlatpakMetadata } from '$lib/flatpak/metadata';
	import {
		describePermissions,
		type PermissionItem,
		type SectionId
	} from '$lib/flatpak/permissions';
	import { cn } from '$lib/utils/shadcn';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import MonitorIcon from '@lucide/svelte/icons/monitor';
	import Volume2Icon from '@lucide/svelte/icons/volume-2';
	import CpuIcon from '@lucide/svelte/icons/cpu';
	import FolderIcon from '@lucide/svelte/icons/folder';
	import WaypointsIcon from '@lucide/svelte/icons/waypoints';
	import KeyRoundIcon from '@lucide/svelte/icons/key-round';
	import WrenchIcon from '@lucide/svelte/icons/wrench';
	import UsbIcon from '@lucide/svelte/icons/usb';
	import VariableIcon from '@lucide/svelte/icons/variable';
	import ShapesIcon from '@lucide/svelte/icons/shapes';
	import ShieldAlertIcon from '@lucide/svelte/icons/shield-alert';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
	import CircleMinusIcon from '@lucide/svelte/icons/circle-minus';
	import InfoIcon from '@lucide/svelte/icons/info';

	let { meta }: { meta: FlatpakMetadata } = $props();

	const summary = $derived(describePermissions(meta));
	const isRuntime = $derived(meta.kind === 'runtime');

	const RANK: Record<string, number> = { high: 2, warning: 1, notice: 0 };
	/** Most severe granted entry of a section: colors its icon. */
	function sectionSeverity(items: PermissionItem[]): 'high' | 'warning' | 'notice' | 'removed' {
		const granted = items.filter((i) => !i.removed);
		if (granted.length === 0) return 'removed';
		return granted.reduce((a, b) => (RANK[b.severity] > RANK[a.severity] ? b : a)).severity;
	}
	const TILE: Record<ReturnType<typeof sectionSeverity>, string> = {
		high: 'bg-destructive/12 text-destructive',
		warning: 'bg-warning/12 text-warning',
		notice: 'bg-success/12 text-success',
		removed: 'bg-muted text-muted-foreground'
	};

	const HEADER = { high: 'bg-destructive/6', warning: 'bg-warning/6', notice: 'bg-success/6' };

	const verdict = $derived.by(() => {
		if (summary.counts.high > 0)
			return {
				tone: 'high' as const,
				icon: ShieldAlertIcon,
				title: isRuntime ? 'Broad access declared' : 'Potentially unsafe',
				text: `${summary.counts.high} broad ${summary.counts.high === 1 ? 'permission' : 'permissions'}${summary.counts.warning ? `, ${summary.counts.warning} notable` : ''}`
			};
		if (summary.counts.warning > 0)
			return {
				tone: 'warning' as const,
				icon: TriangleAlertIcon,
				title: 'Some notable access',
				text: `${summary.counts.warning} notable ${summary.counts.warning === 1 ? 'permission' : 'permissions'}`
			};
		return {
			tone: 'notice' as const,
			icon: ShieldCheckIcon,
			title: summary.total === 0 ? 'No permissions' : 'Safe',
			text:
				summary.total === 0
					? isRuntime
						? 'This runtime declares no sandbox permissions.'
						: 'Runs in the default sandbox: no network and no files outside its own data.'
					: 'Only narrowly scoped access.'
		};
	});

	const SECTION_ICONS: Record<SectionId, Component> = {
		network: GlobeIcon,
		display: MonitorIcon,
		audio: Volume2Icon,
		devices: CpuIcon,
		filesystem: FolderIcon,
		dbus: WaypointsIcon,
		services: KeyRoundIcon,
		features: WrenchIcon,
		usb: UsbIcon,
		environment: VariableIcon,
		other: ShapesIcon
	};
</script>

{#snippet marker(item: PermissionItem)}
	{#if item.removed}
		<CircleMinusIcon class="mt-0.5 size-3.5 shrink-0 text-muted-foreground" aria-label="Removed" />
	{:else if item.severity === 'high'}
		<ShieldAlertIcon class="mt-0.5 size-3.5 shrink-0 text-destructive" aria-label="Broad access" />
	{:else if item.severity === 'warning'}
		<TriangleAlertIcon class="mt-0.5 size-3.5 shrink-0 text-warning" aria-label="Notable access" />
	{:else}
		<CircleCheckIcon class="mt-0.5 size-3.5 shrink-0 text-success" aria-hidden="true" />
	{/if}
{/snippet}

<Card.Root class="gap-0 py-0">
	<Card.Header class={cn('flex items-center gap-3 border-b py-4', HEADER[verdict.tone])}>
		<div class={cn('flex size-10 shrink-0 items-center justify-center rounded-xl', TILE[verdict.tone])}>
			<verdict.icon class="size-5" />
		</div>
		<div class="flex min-w-0 flex-1 flex-col">
			<Card.Title>{verdict.title}</Card.Title>
			<Card.Description>{verdict.text}</Card.Description>
		</div>
		<Tooltip.Root>
			<Tooltip.Trigger>
				{#snippet child({ props })}
					<Button {...props} variant="ghost" size="icon-sm" aria-label="About permissions"><InfoIcon /></Button>
				{/snippet}
			</Tooltip.Trigger>
			<Tooltip.Content class="max-w-xs">
				{isRuntime
					? 'Apps do not inherit a runtime’s permissions, only its environment variables.'
					: 'Sandbox access requested in the metadata. Users can still change it with overrides.'}
			</Tooltip.Content>
		</Tooltip.Root>
	</Card.Header>
	{#if summary.sections.length > 0}
		<ul class="flex flex-col divide-y">
			{#each summary.sections as section (section.id)}
				{@const Icon = SECTION_ICONS[section.id]}
				<li class="flex items-start gap-3 px-4 py-3" aria-label={section.title}>
					<div
						class={cn(
							'flex size-8 shrink-0 items-center justify-center rounded-lg',
							TILE[sectionSeverity(section.items)]
						)}
					>
						<Icon class="size-4" />
					</div>
					<div class="flex min-w-0 flex-1 flex-col gap-1.5">
						<h3 class="text-sm font-medium">{section.title}</h3>
						<ul class="flex flex-col gap-1.5">
							{#each section.items as item (item.id)}
								<li class="flex items-start gap-2" title={item.token}>
									{@render marker(item)}
									<div class="flex min-w-0 flex-1 flex-col gap-0.5">
										<div class="flex flex-wrap items-center gap-1.5">
											<span
												class={cn(
													'break-all',
													item.mono ? 'font-mono text-xs' : 'text-sm',
													item.removed && 'text-muted-foreground line-through',
													item.severity === 'high' && !item.removed && 'font-medium'
												)}
											>
												{item.label}
											</span>
											{#if item.removed}
												<Badge variant="outline">Removed</Badge>
											{:else if item.access}
												<Badge variant="outline">{item.access}</Badge>
											{/if}
											{#if item.condition}
												<Badge variant="secondary">{item.condition}</Badge>
											{/if}
										</div>
										{#if item.detail}
											<p
												class={cn(
													'text-xs text-muted-foreground',
													item.detailMono ? 'font-mono break-all' : 'break-words'
												)}
											>
												{item.detail}
											</p>
										{/if}
										{#if item.problem}
											<p class="text-xs text-destructive">{item.problem}</p>
										{/if}
									</div>
								</li>
							{/each}
						</ul>
					</div>
				</li>
			{/each}
		</ul>
	{/if}
</Card.Root>
