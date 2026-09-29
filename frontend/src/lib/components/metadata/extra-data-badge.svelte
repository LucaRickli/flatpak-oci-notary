<script lang="ts">
	import { Badge } from '$lib/components/ui/badge';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import CloudDownloadIcon from '@lucide/svelte/icons/cloud-download';

	/** Shows nothing unless `hasExtraData` (Image.hasExtraData / Package.hasExtraData). */
	let { hasExtraData }: { hasExtraData: boolean } = $props();
</script>

{#if hasExtraData}
	<Tooltip.Root>
		<Tooltip.Trigger>
			{#snippet child({ props })}
				<!-- Darker text than the stock destructive badge in light mode: 12px text on the tinted
				     background needs 4.5:1 (WCAG AA); dark mode already passes. -->
				<Badge variant="destructive" {...props} class="text-red-700 dark:text-destructive">
					<CloudDownloadIcon data-icon="inline-start" />External download
				</Badge>
			{/snippet}
		</Tooltip.Trigger>
		<Tooltip.Content class="max-w-xs">
			Installing downloads extra data from URLs outside the registry ([Extra Data] in the flatpak metadata).
		</Tooltip.Content>
	</Tooltip.Root>
{/if}
