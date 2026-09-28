<script lang="ts">
	import { goto } from '$app/navigation';
	import PageHeader from '$lib/components/page-header.svelte';
	import RegistryForm from '$lib/components/registry-form.svelte';
</script>

<PageHeader
	title="Add registry"
	description="Connect an upstream OCI registry. Use “Test connection” to check credentials and catalog support before saving."
/>

<RegistryForm onsaved={(r) => goto(`/registries/${r.id}`)} oncancel={() => goto('/registries')} />
