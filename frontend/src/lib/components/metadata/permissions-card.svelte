<script lang="ts">
	import type { Component } from 'svelte';
	import * as Card from '$lib/components/ui/card';
	import * as Item from '$lib/components/ui/item';
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

	let { meta }: { meta: FlatpakMetadata } = $props();

	const summary = $derived(describePermissions(meta));
	const isRuntime = $derived(meta.kind === 'runtime');

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
		<CircleMinusIcon class="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-label="Removed" />
	{:else if item.severity === 'high'}
		<ShieldAlertIcon class="mt-0.5 size-4 shrink-0 text-destructive" aria-label="Broad access" />
	{:else if item.severity === 'warning'}
		<TriangleAlertIcon class="mt-0.5 size-4 shrink-0 text-foreground" aria-label="Notable access" />
	{:else}
		<CircleCheckIcon class="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
	{/if}
{/snippet}

<Card.Root>
	<Card.Header>
		<Card.Title>Permissions</Card.Title>
		<Card.Description>
			{#if isRuntime}
				Apps do not inherit a runtime’s permissions, only its environment variables.
			{:else}
				Sandbox access requested in the metadata. Users can still change it with overrides.
			{/if}
		</Card.Description>
		{#if summary.counts.high + summary.counts.warning > 0}
			<Card.Action class="flex flex-wrap justify-end gap-1">
				{#if summary.counts.high > 0}
					<Badge variant="destructive"><ShieldAlertIcon data-icon="inline-start" />{summary.counts.high} broad</Badge>
				{/if}
				{#if summary.counts.warning > 0}
					<Badge variant="secondary"><TriangleAlertIcon data-icon="inline-start" />{summary.counts.warning} notable</Badge>
				{/if}
			</Card.Action>
		{/if}
	</Card.Header>
	<Card.Content class="flex flex-col gap-4">
		{#if summary.total === 0}
			<Item.Root variant="muted" size="sm">
				<Item.Media variant="icon"><ShieldCheckIcon /></Item.Media>
				<Item.Content>
					<Item.Title>No permissions requested</Item.Title>
					<Item.Description class="line-clamp-none">
						{isRuntime
							? 'This runtime declares no sandbox permissions.'
							: 'Runs in the default sandbox: no network, no files outside its own data folder, and D-Bus limited to portals and its own names.'}
					</Item.Description>
				</Item.Content>
			</Item.Root>
		{/if}
		{#each summary.sections as section (section.id)}
			{@const Icon = SECTION_ICONS[section.id]}
			<section class="flex flex-col gap-1" aria-label={section.title}>
				<h3 class="flex items-center gap-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">
					<Icon class="size-3.5" />{section.title}
				</h3>
				<ul class="flex flex-col divide-y">
					{#each section.items as item (item.id)}
						<li class="flex items-start gap-3 py-2" title={item.token}>
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
			</section>
		{/each}
	</Card.Content>
</Card.Root>
