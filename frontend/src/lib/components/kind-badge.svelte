<script lang="ts">
	import { Badge } from '$lib/components/ui/badge';
	import { RefKind } from '$lib/api';
	import { kindLabel } from '$lib/format';
	import AppWindowIcon from '@lucide/svelte/icons/app-window';
	import LayersIcon from '@lucide/svelte/icons/layers';

	let { kind, class: className }: { kind: RefKind; class?: string } = $props();
</script>

{#if kind === RefKind.APP}
	<Badge variant="info" class={className}><AppWindowIcon data-icon="inline-start" />{kindLabel(kind)}</Badge>
{:else if kind === RefKind.RUNTIME}
	<Badge variant="muted" class={className}><LayersIcon data-icon="inline-start" />{kindLabel(kind)}</Badge>
{:else}
	<Badge variant="outline" class={className}>{kindLabel(kind)}</Badge>
{/if}
