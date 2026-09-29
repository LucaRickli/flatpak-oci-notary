<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import * as Collapsible from '$lib/components/ui/collapsible';
	import { Button } from '$lib/components/ui/button';
	import CodeBlock from './code-block.svelte';
	import type { Repository } from '$lib/api';
	import { remoteAddCommand } from '$lib/remote';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import InfoIcon from '@lucide/svelte/icons/info';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';

	let { repository }: { repository: Repository } = $props();

	const INDEX_QUERY = '?label:org.flatpak.ref:exists=1&architecture=amd64&os=linux&tag=latest';

	const remote = $derived(repository.urls?.remote ?? '');
	const flatpakrepo = $derived(repository.urls?.flatpakrepo ?? '');
	const index = $derived(repository.urls?.index ?? '');
</script>

{#snippet step(n: number, title: string)}
	<h3 class="flex items-center gap-2 text-sm font-medium">
		<span class="flex size-5 items-center justify-center rounded-full bg-primary/10 text-xs font-semibold text-primary">
			{n}
		</span>
		{title}
	</h3>
{/snippet}

<Card.Root>
	<Card.Header>
		<Card.Title>Use this repository</Card.Title>
		<Card.Action>
			<Tooltip.Root>
				<Tooltip.Trigger>
					{#snippet child({ props })}
						<Button {...props} variant="ghost" size="icon-sm" aria-label="About tags"><InfoIcon /></Button>
					{/snippet}
				</Tooltip.Trigger>
				<Tooltip.Content class="max-w-xs">
					Flatpak follows the latest tag. To follow another tag, append #&lt;tag&gt; to the URL, e.g. {remote}#beta.
				</Tooltip.Content>
			</Tooltip.Root>
		</Card.Action>
	</Card.Header>
	<Card.Content class="flex flex-col gap-5">
		<section class="flex flex-col gap-2">
			{@render step(1, 'Add the remote')}
			<CodeBlock code={remoteAddCommand(repository)} />
			<div class="flex flex-wrap items-center gap-2">
				<Button variant="outline" size="sm" href={flatpakrepo} download="{repository.slug}.flatpakrepo" data-sveltekit-reload>
					<DownloadIcon data-icon="inline-start" />.flatpakrepo file
				</Button>
				<Button variant="ghost" size="sm" href={index + INDEX_QUERY} target="_blank" rel="external noopener">
					<ExternalLinkIcon data-icon="inline-start" />Raw index
				</Button>
			</div>
		</section>
		<section class="flex flex-col gap-2">
			{@render step(2, 'Install apps')}
			<CodeBlock code={`flatpak install ${repository.slug} <app-id>`} />
		</section>
		<Collapsible.Root>
			<Collapsible.Trigger>
				{#snippet child({ props })}
					<Button {...props} variant="link" size="sm" class="px-0 text-muted-foreground">
						Or add it from the .flatpakrepo file<ChevronDownIcon data-icon="inline-end" />
					</Button>
				{/snippet}
			</Collapsible.Trigger>
			<Collapsible.Content>
				<CodeBlock code={`flatpak remote-add --if-not-exists ${repository.slug} ${flatpakrepo}`} class="mt-1" />
			</Collapsible.Content>
		</Collapsible.Root>
	</Card.Content>
</Card.Root>
