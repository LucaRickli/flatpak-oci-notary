<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { mode, toggleMode } from 'mode-watcher';
	import SunIcon from '@lucide/svelte/icons/sun';
	import MoonIcon from '@lucide/svelte/icons/moon';
</script>

<Button variant="ghost" size="icon-sm" onclick={toggleMode} aria-label="Toggle dark mode" title="Toggle dark mode">
	{#if mode.current === 'dark'}<SunIcon />{:else}<MoonIcon />{/if}
</Button>
