<script lang="ts">
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import type { MessageInitShape } from '@bufbuild/protobuf';
	import * as Card from '$lib/components/ui/card';
	import * as Field from '$lib/components/ui/field';
	import * as Alert from '$lib/components/ui/alert';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { Input } from '$lib/components/ui/input';
	import { Switch } from '$lib/components/ui/switch';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { Spinner } from '$lib/components/ui/spinner';
	import LinesTextarea from './lines-textarea.svelte';
	import {
		AuthType,
		registryClient,
		type Registry,
		type RegistryInputSchema,
		type TestRegistryResponse
	} from '$lib/api';
	import { reportError } from '$lib/session.svelte';
	import PlugIcon from '@lucide/svelte/icons/plug';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import CheckIcon from '@lucide/svelte/icons/check';
	import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
	import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

	let {
		registry,
		onsaved,
		oncancel
	}: {
		/** Existing registry to edit; omit to create a new one. */
		registry?: Registry;
		onsaved: (registry: Registry) => void;
		oncancel?: () => void;
	} = $props();

	// The form is seeded once; later updates of `registry` (e.g. sync polling) must not reset edits.
	const initial = untrack(() => registry);
	let form = $state({
		name: initial?.name ?? '',
		url: initial?.url ?? '',
		insecure: initial?.insecure ?? false,
		authType: initial?.authType === AuthType.BASIC ? AuthType.BASIC : AuthType.ANONYMOUS,
		username: initial?.username ?? '',
		password: '',
		useCatalog: initial?.useCatalog ?? false,
		repositories: [...(initial?.repositories ?? [])],
		repositoryPatterns: [...(initial?.repositoryPatterns ?? [])],
		tagPatterns: [...(initial?.tagPatterns ?? [])],
		syncIntervalMinutes: initial?.syncIntervalMinutes ?? 60
	});

	let saving = $state(false);
	let testing = $state(false);
	let testResult = $state<TestRegistryResponse>();

	const isBasic = $derived(form.authType === AuthType.BASIC);
	const nothingToIndex = $derived(!form.useCatalog && form.repositories.length === 0);

	function buildInput(): MessageInitShape<typeof RegistryInputSchema> {
		const f = $state.snapshot(form);
		return {
			...f,
			name: f.name.trim(),
			url: f.url.trim().replace(/\/+$/, ''),
			username: f.authType === AuthType.BASIC ? f.username.trim() : '',
			// Unset keeps the stored password.
			password: f.authType === AuthType.BASIC && f.password ? f.password : undefined,
			syncIntervalMinutes: Math.max(0, Math.floor(Number(f.syncIntervalMinutes) || 0))
		};
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		saving = true;
		try {
			const input = buildInput();
			const res = registry
				? await registryClient.updateRegistry({ id: registry.id, registry: input })
				: await registryClient.createRegistry({ registry: input });
			form.password = '';
			toast.success(registry ? 'Registry saved' : 'Registry added');
			if (res.registry) onsaved(res.registry);
		} catch (err) {
			reportError(err, 'Could not save registry');
		} finally {
			saving = false;
		}
	}

	async function test() {
		testing = true;
		testResult = undefined;
		try {
			testResult = await registryClient.testRegistry({ registry: buildInput(), id: registry?.id });
		} catch (err) {
			reportError(err, 'Connection test failed');
		} finally {
			testing = false;
		}
	}

	function addRepository(name: string) {
		if (!form.repositories.includes(name)) form.repositories = [...form.repositories, name];
	}
</script>

<form onsubmit={save}>
	<Card.Root>
		<Card.Content>
			<Field.FieldGroup>
				<Field.FieldSet>
					<Field.FieldLegend>Connection</Field.FieldLegend>
					<Field.FieldDescription>Where the upstream OCI registry lives and how to access it.</Field.FieldDescription>
					<Field.FieldGroup>
						<div class="grid gap-4 md:grid-cols-2">
							<Field.Field>
								<Field.FieldLabel for="registry-name">Name</Field.FieldLabel>
								<Input id="registry-name" bind:value={form.name} placeholder="GitHub Container Registry" required />
							</Field.Field>
							<Field.Field>
								<Field.FieldLabel for="registry-url">URL</Field.FieldLabel>
								<Input
									id="registry-url"
									type="url"
									bind:value={form.url}
									placeholder="https://ghcr.io"
									pattern="https?://.+"
									required
								/>
								<Field.FieldDescription>Base URL of the registry, without /v2.</Field.FieldDescription>
							</Field.Field>
						</div>
						<Field.Field orientation="horizontal">
							<Switch id="registry-insecure" bind:checked={form.insecure} />
							<Field.FieldContent>
								<Field.FieldLabel for="registry-insecure">Insecure</Field.FieldLabel>
								<Field.FieldDescription>Allow plain HTTP and skip TLS certificate verification.</Field.FieldDescription>
							</Field.FieldContent>
						</Field.Field>
						<Field.Field>
							<Field.FieldLabel id="registry-auth-label">Authentication</Field.FieldLabel>
							<ToggleGroup.Root
								type="single"
								variant="outline"
								aria-labelledby="registry-auth-label"
								value={String(form.authType)}
								onValueChange={(v) => v && (form.authType = Number(v) as AuthType)}
							>
								<ToggleGroup.Item value={String(AuthType.ANONYMOUS)}>Anonymous</ToggleGroup.Item>
								<ToggleGroup.Item value={String(AuthType.BASIC)}>Username &amp; password</ToggleGroup.Item>
							</ToggleGroup.Root>
						</Field.Field>
						{#if isBasic}
							<div class="grid gap-4 md:grid-cols-2">
								<Field.Field>
									<Field.FieldLabel for="registry-username">Username</Field.FieldLabel>
									<Input id="registry-username" bind:value={form.username} autocomplete="off" required />
								</Field.Field>
								<Field.Field>
									<Field.FieldLabel for="registry-password">Password or token</Field.FieldLabel>
									<Input
										id="registry-password"
										type="password"
										bind:value={form.password}
										autocomplete="new-password"
										placeholder={registry?.hasPassword ? '•••••••• (stored)' : ''}
										required={!registry?.hasPassword}
									/>
									<Field.FieldDescription>
										{registry?.hasPassword
											? 'A password is stored. Leave empty to keep it.'
											: 'For ghcr.io, use a personal access token with read:packages.'}
									</Field.FieldDescription>
								</Field.Field>
							</div>
						{/if}
						<div class="flex flex-col gap-3">
							<div>
								<Button type="button" variant="outline" onclick={test} disabled={testing || !form.url}>
									{#if testing}<Spinner data-icon="inline-start" />{:else}<PlugIcon data-icon="inline-start" />{/if}
									Test connection
								</Button>
							</div>
							{#if testResult}
								{@const result = testResult}
								<Alert.Root variant={result.ok ? 'default' : 'destructive'}>
									{#if result.ok}<CircleCheckIcon />{:else}<CircleAlertIcon />{/if}
									<Alert.Title>{result.ok ? 'Connection successful' : 'Connection failed'}</Alert.Title>
									<Alert.Description>
										{#if !result.ok}
											<p>{result.error}</p>
										{:else}
											<div class="flex flex-col gap-2">
												<div>
													{#if result.catalogSupported}
														<Badge variant="secondary">Catalog supported</Badge>
													{:else}
														<Badge variant="outline">Catalog not supported</Badge>
														<span>List repositories explicitly below.</span>
													{/if}
												</div>
												{#if result.sampleRepositories.length > 0}
													<p>Sample repositories — click to add to the explicit list:</p>
													<div class="flex max-h-48 flex-wrap gap-1.5 overflow-y-auto">
														{#each result.sampleRepositories as name (name)}
															{@const added = form.repositories.includes(name)}
															<Button
																type="button"
																variant="outline"
																size="xs"
																class="font-mono"
																disabled={added}
																onclick={() => addRepository(name)}
															>
																{#if added}<CheckIcon data-icon="inline-start" />{:else}<PlusIcon data-icon="inline-start" />{/if}
																{name}
															</Button>
														{/each}
													</div>
												{/if}
											</div>
										{/if}
									</Alert.Description>
								</Alert.Root>
							{/if}
						</div>
					</Field.FieldGroup>
				</Field.FieldSet>

				<Field.FieldSeparator />

				<Field.FieldSet>
					<Field.FieldLegend>Discovery</Field.FieldLegend>
					<Field.FieldDescription>
						Which repositories and tags to index. Only images carrying the
						<code class="font-mono text-xs">org.flatpak.ref</code> label are picked up.
					</Field.FieldDescription>
					<Field.FieldGroup>
						<Field.Field orientation="horizontal">
							<Switch id="registry-catalog" bind:checked={form.useCatalog} />
							<Field.FieldContent>
								<Field.FieldLabel for="registry-catalog">Discover via catalog</Field.FieldLabel>
								<Field.FieldDescription>
									List repositories with <code class="font-mono text-xs">/v2/_catalog</code>. Not supported by
									ghcr.io or Docker Hub.
								</Field.FieldDescription>
							</Field.FieldContent>
						</Field.Field>
						<Field.Field>
							<Field.FieldLabel for="registry-repositories">Repositories</Field.FieldLabel>
							<LinesTextarea
								id="registry-repositories"
								bind:value={form.repositories}
								placeholder={'myorg/org.example.App\nmyorg/org.example.Other'}
							/>
							{#if nothingToIndex}
								<Field.FieldDescription class="flex items-center gap-1.5">
									<TriangleAlertIcon class="size-3.5 shrink-0" />
									Enable catalog discovery or list at least one repository, otherwise nothing gets indexed.
								</Field.FieldDescription>
							{:else}
								<Field.FieldDescription>Explicit repository paths, one per line. Indexed in addition to the catalog.</Field.FieldDescription>
							{/if}
						</Field.Field>
						<div class="grid gap-4 md:grid-cols-2">
							<Field.Field>
								<Field.FieldLabel for="registry-repo-patterns">Repository patterns</Field.FieldLabel>
								<LinesTextarea
									id="registry-repo-patterns"
									bind:value={form.repositoryPatterns}
									placeholder={'flathub/*\nmyorg/org.example.*'}
								/>
								<Field.FieldDescription>
									Globs limiting catalog results (<code class="font-mono">*</code> also matches
									<code class="font-mono">/</code>). Empty = all.
								</Field.FieldDescription>
							</Field.Field>
							<Field.Field>
								<Field.FieldLabel for="registry-tag-patterns">Tag patterns</Field.FieldLabel>
								<LinesTextarea id="registry-tag-patterns" bind:value={form.tagPatterns} placeholder={'latest\nstable-*'} />
								<Field.FieldDescription>Globs for tags to index. Empty = all tags.</Field.FieldDescription>
							</Field.Field>
						</div>
					</Field.FieldGroup>
				</Field.FieldSet>

				<Field.FieldSeparator />

				<Field.FieldSet>
					<Field.FieldLegend>Sync</Field.FieldLegend>
					<Field.Field class="max-w-xs">
						<Field.FieldLabel for="registry-interval">Sync interval (minutes)</Field.FieldLabel>
						<Input id="registry-interval" type="number" min="0" step="1" bind:value={form.syncIntervalMinutes} />
						<Field.FieldDescription>0 disables periodic syncing (manual only).</Field.FieldDescription>
					</Field.Field>
				</Field.FieldSet>
			</Field.FieldGroup>
		</Card.Content>
		<Card.Footer class="justify-end gap-2 border-t">
			{#if oncancel}
				<Button type="button" variant="ghost" onclick={oncancel}>Cancel</Button>
			{/if}
			<Button type="submit" disabled={saving}>
				{#if saving}<Spinner data-icon="inline-start" />{/if}
				{registry ? 'Save changes' : 'Add registry'}
			</Button>
		</Card.Footer>
	</Card.Root>
</form>
