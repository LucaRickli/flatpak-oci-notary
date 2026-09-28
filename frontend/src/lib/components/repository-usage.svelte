<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import CodeBlock from './code-block.svelte';
	import type { Repository } from '$lib/api';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';

	let { repository }: { repository: Repository } = $props();

	const INDEX_QUERY = '?label:org.flatpak.ref:exists=1&architecture=amd64&os=linux&tag=latest';

	const remote = $derived(repository.urls?.remote ?? '');
	const flatpakrepo = $derived(repository.urls?.flatpakrepo ?? '');
	const index = $derived(repository.urls?.index ?? '');
</script>

<Card.Root>
	<Card.Header>
		<Card.Title>Use this repository</Card.Title>
		<Card.Description>Add it as a flatpak remote on any machine that can reach this server.</Card.Description>
	</Card.Header>
	<Card.Content class="flex flex-col gap-5">
		<section class="flex flex-col gap-2">
			<h3 class="text-sm font-medium">Add the remote</h3>
			<CodeBlock code={`flatpak remote-add --if-not-exists --no-gpg-verify ${repository.slug} ${remote}`} />
			<p class="text-xs text-muted-foreground">
				Flatpak follows the <code class="font-mono">latest</code> tag. To follow another tag, append
				<code class="font-mono">#&lt;tag&gt;</code> to the URL, e.g.
				<code class="font-mono break-all">{remote}#beta</code>.
			</p>
		</section>

		<section class="flex flex-col gap-2">
			<h3 class="text-sm font-medium">Or use the .flatpakrepo file</h3>
			<CodeBlock code={`flatpak remote-add --if-not-exists ${repository.slug} ${flatpakrepo}`} />
			<div class="flex flex-wrap gap-2">
				<Button variant="outline" size="sm" href={flatpakrepo} download="{repository.slug}.flatpakrepo" data-sveltekit-reload>
					<DownloadIcon data-icon="inline-start" />Download .flatpakrepo
				</Button>
				<Button variant="ghost" size="sm" href={index + INDEX_QUERY} target="_blank" rel="external noopener">
					<ExternalLinkIcon data-icon="inline-start" />Raw index (amd64, latest)
				</Button>
			</div>
		</section>

		<section class="flex flex-col gap-2">
			<h3 class="text-sm font-medium">Install apps</h3>
			<CodeBlock code={`flatpak install ${repository.slug} <app-id>`} />
		</section>
	</Card.Content>
</Card.Root>
