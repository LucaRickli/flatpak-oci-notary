<script lang="ts">
  import { page } from "$app/state";
  import * as Sidebar from "$lib/components/ui/sidebar";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu";
  import * as Avatar from "$lib/components/ui/avatar";
  import { systemClient } from "$lib/api";
  import { session, reportError } from "$lib/session.svelte";
  import { initials } from "$lib/format";
  import LayoutDashboardIcon from "@lucide/svelte/icons/layout-dashboard";
  import ServerIcon from "@lucide/svelte/icons/server";
  import PackageIcon from "@lucide/svelte/icons/package";
  import LibraryIcon from "@lucide/svelte/icons/library";
  import StampIcon from "@lucide/svelte/icons/stamp";
  import ChevronsUpDownIcon from "@lucide/svelte/icons/chevrons-up-down";
  import LogOutIcon from "@lucide/svelte/icons/log-out";

  const nav = [
    { title: "Dashboard", href: "/", icon: LayoutDashboardIcon },
    { title: "Registries", href: "/registries", icon: ServerIcon },
    { title: "Packages", href: "/packages", icon: PackageIcon },
    { title: "Repositories", href: "/repositories", icon: LibraryIcon },
  ];

  const sidebar = Sidebar.useSidebar();

  function isActive(href: string) {
    const path = page.url.pathname;
    return href === "/"
      ? path === "/"
      : path === href || path.startsWith(`${href}/`);
  }

  const user = $derived(session.me?.user);
  const displayName = $derived(
    user?.name || user?.email || user?.subject || "Signed in",
  );

  async function signOut() {
    try {
      await systemClient.logout({});
      location.reload();
    } catch (err) {
      reportError(err, "Could not sign out");
    }
  }

  const semverRegex = /^(\d+\.\d+\.\d+)(?:-.+)?$/;
</script>

<Sidebar.Root collapsible="icon">
  <Sidebar.Header>
    <Sidebar.Menu>
      <Sidebar.MenuItem>
        <Sidebar.MenuButton size="lg">
          {#snippet child({ props })}
            <a href="/" {...props} onclick={() => sidebar.setOpenMobile(false)}>
              <div
                class="flex aspect-square size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground"
              >
                <StampIcon class="size-4" />
              </div>
              <div class="grid flex-1 text-left text-sm leading-tight">
                <span class="truncate font-semibold">Flatpak OCI Notary</span>
                <span class="truncate text-xs text-muted-foreground">
                  {session.info?.version
                    ? `${semverRegex.test(session.info.version) ? "v" : ""}${session.info.version}`
                    : "Unknown Version"}
                </span>
              </div>
            </a>
          {/snippet}
        </Sidebar.MenuButton>
      </Sidebar.MenuItem>
    </Sidebar.Menu>
  </Sidebar.Header>

  <Sidebar.Content>
    <Sidebar.Group>
      <Sidebar.GroupLabel>Manage</Sidebar.GroupLabel>
      <Sidebar.GroupContent>
        <Sidebar.Menu>
          {#each nav as item (item.href)}
            <Sidebar.MenuItem>
              <Sidebar.MenuButton
                isActive={isActive(item.href)}
                tooltipContent={item.title}
              >
                {#snippet child({ props })}
                  <a
                    href={item.href}
                    {...props}
                    onclick={() => sidebar.setOpenMobile(false)}
                  >
                    <item.icon />
                    <span>{item.title}</span>
                  </a>
                {/snippet}
              </Sidebar.MenuButton>
            </Sidebar.MenuItem>
          {/each}
        </Sidebar.Menu>
      </Sidebar.GroupContent>
    </Sidebar.Group>
  </Sidebar.Content>

  {#if session.me?.authEnabled}
    <Sidebar.Footer>
      <Sidebar.Menu>
        <Sidebar.MenuItem>
          <DropdownMenu.Root>
            <DropdownMenu.Trigger>
              {#snippet child({ props })}
                <Sidebar.MenuButton size="lg" {...props}>
                  <Avatar.Root class="size-8 rounded-lg">
                    <Avatar.Fallback class="rounded-lg"
                      >{initials(displayName)}</Avatar.Fallback
                    >
                  </Avatar.Root>
                  <div class="grid flex-1 text-left text-sm leading-tight">
                    <span class="truncate font-medium">{displayName}</span>
                    {#if user?.email && user.email !== displayName}
                      <span class="truncate text-xs text-muted-foreground"
                        >{user.email}</span
                      >
                    {/if}
                  </div>
                  <ChevronsUpDownIcon class="ml-auto" />
                </Sidebar.MenuButton>
              {/snippet}
            </DropdownMenu.Trigger>
            <DropdownMenu.Content
              class="w-(--bits-dropdown-menu-anchor-width) min-w-56"
              side={sidebar.isMobile ? "bottom" : "right"}
              align="end"
            >
              <DropdownMenu.Group>
                <DropdownMenu.Label class="font-normal">
                  <div class="grid text-sm leading-tight">
                    <span class="truncate font-medium">{displayName}</span>
                    {#if user?.email}
                      <span class="truncate text-xs text-muted-foreground"
                        >{user.email}</span
                      >
                    {/if}
                  </div>
                </DropdownMenu.Label>
              </DropdownMenu.Group>
              <DropdownMenu.Separator />
              <DropdownMenu.Group>
                <DropdownMenu.Item onclick={signOut}>
                  <LogOutIcon />
                  Sign out
                </DropdownMenu.Item>
              </DropdownMenu.Group>
            </DropdownMenu.Content>
          </DropdownMenu.Root>
        </Sidebar.MenuItem>
      </Sidebar.Menu>
    </Sidebar.Footer>
  {/if}
  <Sidebar.Rail />
</Sidebar.Root>
