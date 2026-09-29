<script lang="ts" module>
	export interface RepositoryMeta {
		slug: string;
		title: string;
		description: string;
		homepage: string;
		/** Empty = none selected. */
		registryId: string;
	}
</script>

<script lang="ts">
	import * as Field from '$lib/components/ui/field';
	import * as Select from '$lib/components/ui/select';
	import { Input } from '$lib/components/ui/input';
	import { Textarea } from '$lib/components/ui/textarea';
	import type { Registry } from '$lib/api';
	import { SLUG_PATTERN, slugify } from '$lib/format';

	let {
		value = $bindable(),
		registries,
		autoSlug = false
	}: {
		value: RepositoryMeta;
		registries: Registry[];
		/** Derive the slug from the title until the slug is edited by hand (new repositories). */
		autoSlug?: boolean;
	} = $props();

	let slugEdited = $state(false);
	const slugInvalid = $derived(value.slug !== '' && !SLUG_PATTERN.test(value.slug));
	const registryName = $derived(registries.find((r) => r.id === value.registryId)?.name);
</script>

<Field.FieldGroup>
	<div class="grid gap-4 md:grid-cols-2">
		<Field.Field>
			<Field.FieldLabel for="repo-title">Title</Field.FieldLabel>
			<Input
				id="repo-title"
				bind:value={value.title}
				placeholder="Example Apps"
				required
				oninput={() => {
					if (autoSlug && !slugEdited) value.slug = slugify(value.title);
				}}
			/>
		</Field.Field>
		<Field.Field data-invalid={slugInvalid || undefined}>
			<Field.FieldLabel for="repo-slug">Slug</Field.FieldLabel>
			<Input
				id="repo-slug"
				bind:value={value.slug}
				class="font-mono"
				placeholder="example-apps"
				required
				aria-invalid={slugInvalid || undefined}
				oninput={() => (slugEdited = true)}
			/>
			<Field.FieldDescription>
				{slugInvalid
					? 'Lowercase letters, digits, “.”, “_” and “-”; must start with a letter or digit.'
					: 'Part of the remote URL and the default remote name.'}
			</Field.FieldDescription>
		</Field.Field>
	</div>
	<Field.Field>
		<Field.FieldLabel for="repo-description">Description</Field.FieldLabel>
		<Textarea id="repo-description" bind:value={value.description} placeholder="Optional" class="min-h-16" />
	</Field.Field>
	<Field.Field>
		<Field.FieldLabel for="repo-homepage">Homepage</Field.FieldLabel>
		<Input id="repo-homepage" type="url" bind:value={value.homepage} placeholder="https://example.com (optional)" />
	</Field.Field>
	<Field.Field>
		<Field.FieldLabel for="repo-registry">Registry</Field.FieldLabel>
		<Select.Root
			type="single"
			value={value.registryId}
			onValueChange={(v) => (value.registryId = v)}
		>
			<Select.Trigger id="repo-registry" class="w-full md:w-80">
				{registryName ?? 'Select a registry'}
			</Select.Trigger>
			<Select.Content>
				<Select.Group>
					{#each registries as r (r.id)}
						<Select.Item value={r.id} label={r.name}>
							<span class="flex flex-col">
								<span>{r.name}</span>
								<span class="font-mono text-xs text-muted-foreground">{r.url}</span>
							</span>
						</Select.Item>
					{/each}
				</Select.Group>
			</Select.Content>
		</Select.Root>
		<Field.FieldDescription>
			A repository serves images from exactly one registry: flatpak's OCI index format has a single
			<code class="font-mono">Registry</code> base URL for all images of a remote. Create one repository per registry.
		</Field.FieldDescription>
	</Field.Field>
</Field.FieldGroup>
