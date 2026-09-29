<script lang="ts">
	import { Badge } from '$lib/components/ui/badge';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import CloudDownloadIcon from '@lucide/svelte/icons/cloud-download';

	/** Shows nothing unless `hasExtraData` (Image.hasExtraData / Package.hasExtraData). */
	let { hasExtraData, compact = false }: { hasExtraData: boolean; /** Icon only. */ compact?: boolean } =
		$props();
</script>

{#if hasExtraData}
	<Tooltip.Root>
		<Tooltip.Trigger>
			{#snippet child({ props })}
				<Badge variant="warning" {...props} aria-label={compact ? 'External download' : undefined}>
					<CloudDownloadIcon data-icon={compact ? undefined : 'inline-start'} />{#if !compact}External download{/if}
				</Badge>
			{/snippet}
		</Tooltip.Trigger>
		<Tooltip.Content class="max-w-xs">
			Installing downloads extra data from URLs outside the registry ([Extra Data] in the flatpak metadata).
		</Tooltip.Content>
	</Tooltip.Root>
{/if}
