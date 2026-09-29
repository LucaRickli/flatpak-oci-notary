<script lang="ts">
	import * as Alert from '$lib/components/ui/alert';
	import * as Item from '$lib/components/ui/item';
	import { Badge } from '$lib/components/ui/badge';
	import CopyButton from '$lib/components/copy-button.svelte';
	import type { ExtraData } from '$lib/flatpak/metadata';
	import { formatBytes } from '$lib/format';
	import CloudDownloadIcon from '@lucide/svelte/icons/cloud-download';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';

	/** The [Extra Data] group of flatpak metadata. */
	let { extraData }: { extraData: ExtraData } = $props();

	const count = $derived(extraData.sources.length);
</script>

<div class="flex flex-col gap-2">
	<Alert.Root variant="destructive">
		<CloudDownloadIcon />
		<Alert.Title>
			Installing downloads {count === 1 ? 'a file' : count === 0 ? 'files' : `${count} files`} from outside the registry
		</Alert.Title>
		<Alert.Description>
			<p>
				Flatpak fetches extra data from these URLs during installation and checks it against the checksum. The files are
				not part of the OCI image, and an install script (<code class="font-mono">apply_extra</code>) unpacks them{extraData.noRuntime
					? '.'
					: ' inside the runtime, so the runtime must be installed.'}
			</p>
		</Alert.Description>
	</Alert.Root>

	{#each extraData.sources as source (source.suffix)}
		<Item.Root variant="outline" size="sm">
			<Item.Content class="min-w-0">
				<Item.Title class="line-clamp-none flex-wrap">
					<span class="font-mono text-xs break-all">{source.name}</span>
					{#if source.size !== undefined}
						<Badge variant="secondary">{formatBytes(source.size)}</Badge>
					{/if}
					{#if source.installedSize}
						<Badge variant="outline">{formatBytes(source.installedSize)} installed</Badge>
					{/if}
				</Item.Title>
				<div class="flex flex-col gap-1 text-sm text-muted-foreground">
					{#if source.httpUrl}
						<a
							href={source.httpUrl}
							target="_blank"
							rel="noopener noreferrer nofollow"
							class="inline-flex items-start gap-1 font-mono text-xs break-all text-foreground underline-offset-4 hover:underline"
						>
							{source.uri}<ExternalLinkIcon class="mt-0.5 size-3 shrink-0" />
						</a>
					{:else}
						<span class="font-mono text-xs break-all text-foreground">{source.uri || '(no URI)'}</span>
					{/if}
					{#if source.host}
						<span class="text-xs">
							Downloaded from <span class="font-mono font-medium break-all text-foreground">{source.host}</span>
						</span>
					{/if}
					{#if source.checksum}
						<span class="flex min-w-0 items-center gap-1 text-xs">
							<span class="shrink-0">SHA-256</span>
							<span class="truncate font-mono" title={source.checksum}>{source.checksum}</span>
							<CopyButton value={source.checksum} label="Copy checksum" />
						</span>
					{/if}
					{#each source.problems as problem (problem)}
						<span class="text-xs text-destructive">{problem}</span>
					{/each}
				</div>
			</Item.Content>
		</Item.Root>
	{/each}
</div>
