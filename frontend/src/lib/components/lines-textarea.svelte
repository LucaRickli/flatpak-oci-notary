<script lang="ts">
	import { Textarea } from '$lib/components/ui/textarea';
	import { parseLines } from '$lib/format';
	import type { HTMLTextareaAttributes } from 'svelte/elements';

	/** A textarea editing a string list, one entry per line. */
	let {
		value = $bindable([]),
		...restProps
	}: Omit<HTMLTextareaAttributes, 'value'> & { value?: string[] } = $props();

	let text = $state(value.join('\n'));

	// Pick up changes made from outside (e.g. entries added programmatically)
	// without clobbering what the user is typing.
	$effect(() => {
		const joined = value.join('\n');
		if (parseLines(text).join('\n') !== joined) text = joined;
	});
</script>

<Textarea
	bind:value={text}
	oninput={() => (value = parseLines(text))}
	class="min-h-20 font-mono text-xs"
	spellcheck="false"
	autocapitalize="off"
	{...restProps}
/>
