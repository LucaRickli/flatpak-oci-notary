/**
 * Human-readable, grouped description of the sandbox permissions in flatpak
 * metadata, with a severity tier per entry for broad access. Meanings follow
 * flatpak-metadata(5) and common/flatpak-context.c; there is no score.
 *
 * Pure TypeScript without app imports so it runs under `deno test`.
 */
import {
	XDG_DIRECTORIES,
	permissionEffect,
	type BusName,
	type Condition,
	type FilesystemEntry,
	type FlatpakMetadata,
	type Permission,
	type UsbQuery
} from './metadata.ts';

/**
 * - high: broad access that can read most user data or escape the sandbox
 * - warning: notable access worth a look
 * - notice: common, narrowly scoped access
 */
export type Severity = 'high' | 'warning' | 'notice';

export type SectionId =
	| 'network'
	| 'display'
	| 'audio'
	| 'devices'
	| 'filesystem'
	| 'dbus'
	| 'services'
	| 'features'
	| 'usb'
	| 'environment'
	| 'other';

export interface PermissionItem {
	/** Unique within the description. */
	id: string;
	label: string;
	/** Render the label as code (paths, bus names, variables). */
	mono?: boolean;
	detail?: string;
	/** Render the detail as code (e.g. an environment variable's value). */
	detailMono?: boolean;
	/** The entry as written in the metadata, e.g. "sockets=x11". */
	token: string;
	severity: Severity;
	/** Access explicitly removed (a negated entry, policy "none", unset variable). */
	removed?: boolean;
	/** Short access qualifier, e.g. "read-only". */
	access?: string;
	/** For conditional grants, e.g. "only outside a Wayland session". */
	condition?: string;
	/** Why flatpak ignores or rejects the entry. */
	problem?: string;
}

export interface PermissionSection {
	id: SectionId;
	title: string;
	items: PermissionItem[];
}

export interface PermissionSummary {
	sections: PermissionSection[];
	/** Granted (not removed) entries per severity. */
	counts: Record<Severity, number>;
	/** Number of entries, including removed ones. */
	total: number;
}

export const SECTION_TITLES: Record<SectionId, string> = {
	network: 'Network',
	display: 'Display',
	audio: 'Audio',
	devices: 'Devices',
	filesystem: 'Files',
	dbus: 'D-Bus',
	services: 'Host services',
	features: 'Features',
	usb: 'USB devices',
	environment: 'Environment',
	other: 'Other'
};

interface Known {
	section: SectionId;
	label: string;
	detail: string;
	severity: Severity;
}

const SHARED: Record<string, Known> = {
	network: {
		section: 'network',
		label: 'Network access',
		detail: 'Can connect to the internet and the local network.',
		severity: 'notice'
	},
	ipc: {
		section: 'network',
		label: 'Host IPC namespace',
		detail: 'Shares System V IPC and shared memory with the host (needed for X11 shared memory).',
		severity: 'notice'
	}
};

const SOCKETS: Record<string, Known> = {
	x11: {
		section: 'display',
		label: 'X11 display',
		detail: 'Legacy windowing system: X11 clients can capture other X11 windows and read their input.',
		severity: 'warning'
	},
	'fallback-x11': {
		section: 'display',
		label: 'X11 display as fallback',
		detail: 'X11 only when no Wayland session is available.',
		severity: 'notice'
	},
	wayland: {
		section: 'display',
		label: 'Wayland display',
		detail: 'Shows windows through Wayland, isolated from other apps.',
		severity: 'notice'
	},
	'inherit-wayland-socket': {
		section: 'display',
		label: 'Inherited Wayland socket',
		detail: 'Uses the Wayland socket flatpak was started with.',
		severity: 'notice'
	},
	pulseaudio: {
		section: 'audio',
		label: 'Sound (PulseAudio)',
		detail: 'Plays audio and can record from microphones.',
		severity: 'notice'
	},
	'session-bus': {
		section: 'dbus',
		label: 'Full session bus access',
		detail: 'Can talk to every service on the session bus, which allows escaping the sandbox.',
		severity: 'high'
	},
	'system-bus': {
		section: 'dbus',
		label: 'Full system bus access',
		detail: 'Can talk to every service on the system bus.',
		severity: 'high'
	},
	'ssh-auth': {
		section: 'services',
		label: 'SSH agent',
		detail: 'Can use your SSH keys through the SSH agent.',
		severity: 'warning'
	},
	'gpg-agent': {
		section: 'services',
		label: 'GPG agent',
		detail: 'Can use your GPG keys through gpg-agent.',
		severity: 'warning'
	},
	pcsc: {
		section: 'services',
		label: 'Smart cards',
		detail: 'Can use smart card readers (PC/SC).',
		severity: 'warning'
	},
	cups: {
		section: 'services',
		label: 'Printing (CUPS)',
		detail: 'Talks to the CUPS print server directly.',
		severity: 'notice'
	}
};

const DEVICES: Record<string, Known> = {
	all: {
		section: 'devices',
		label: 'All devices',
		detail: 'Every device node in /dev, including webcams, disks and input devices.',
		severity: 'high'
	},
	dri: {
		section: 'devices',
		label: 'GPU acceleration',
		detail: 'Graphics and compute devices (/dev/dri).',
		severity: 'notice'
	},
	kvm: {
		section: 'devices',
		label: 'Virtualization',
		detail: 'Can run virtual machines (/dev/kvm).',
		severity: 'warning'
	},
	shm: {
		section: 'devices',
		label: 'Host shared memory',
		detail: 'Access to the host’s /dev/shm.',
		severity: 'notice'
	},
	input: {
		section: 'devices',
		label: 'Input devices',
		detail: 'Reads keyboards, mice and game controllers directly (/dev/input).',
		severity: 'warning'
	},
	usb: {
		section: 'devices',
		label: 'Raw USB access',
		detail: 'All USB devices (/dev/bus/usb).',
		severity: 'warning'
	}
};

const FEATURES: Record<string, Known> = {
	devel: {
		section: 'features',
		label: 'Development system calls',
		detail: 'Allows ptrace, perf and other system calls used by debuggers and profilers.',
		severity: 'warning'
	},
	multiarch: {
		section: 'features',
		label: 'Multiarch',
		detail: 'Can run binaries of other architectures, e.g. 32-bit i386.',
		severity: 'notice'
	},
	bluetooth: {
		section: 'features',
		label: 'Bluetooth',
		detail: 'Can use Bluetooth sockets.',
		severity: 'notice'
	},
	canbus: {
		section: 'features',
		label: 'CAN bus',
		detail: 'Can use CAN bus sockets.',
		severity: 'notice'
	},
	'per-app-dev-shm': {
		section: 'features',
		label: 'Per-app /dev/shm',
		detail: 'Shares /dev/shm between all instances of the app.',
		severity: 'notice'
	}
};

/** Human-readable condition of a conditional permission. */
export function describeCondition(c: Condition): string {
	switch (c.name) {
		case 'has-wayland':
			return c.negated ? 'outside a Wayland session' : 'in a Wayland session';
		case 'has-usb-portal':
			return c.negated ? 'without the USB portal' : 'with the USB portal';
		case 'has-input-device':
			return c.negated ? 'on flatpak older than 1.15.6' : 'on flatpak 1.15.6 or newer';
		case 'has-usb-device':
			return c.negated ? 'on flatpak older than 1.16' : 'on flatpak 1.16 or newer';
		case 'true':
			return c.negated ? 'never' : 'always';
		case 'false':
			return c.negated ? 'always' : 'never';
		default:
			return `if “${c.negated ? '!' : ''}${c.name}” (unknown, never true)`;
	}
}

// Home-relative locations where write access allows running code outside
// the sandbox (flatpak overrides, desktop files, autostart, shell profiles).
const ESCAPE_HOME_PATHS = [
	'.local/share/flatpak',
	'.local/share/applications',
	'.config/autostart',
	'.config/systemd',
	'.bashrc',
	'.bash_profile',
	'.profile'
];
const ESCAPE_ABSOLUTE_PATHS = ['/run/docker.sock', '/var/run/docker.sock', '/var/lib/flatpak'];
// Host directories holding users' home directories (/var/home on Fedora Atomic desktops).
const HOME_ROOTS = ['/home', '/var/home'];
// Home-relative locations holding private keys: even read access is broad.
const SECRET_HOME_PATHS = ['.ssh', '.gnupg'];
const XDG_HOME: Record<string, string> = {
	'xdg-data': '.local/share',
	'xdg-config': '.config',
	'xdg-cache': '.cache'
};

/** Whether `path` is, contains or is inside `sensitive`. */
const overlaps = (path: string, sensitive: string) =>
	path === sensitive || sensitive.startsWith(`${path}/`) || path.startsWith(`${sensitive}/`);

/**
 * For an absolute path inside a user's home directory (/home/<user>/..., /var/home/<user>/...,
 * /root/...), the part below it ("" for the home directory itself); the user is unknown, so any
 * user's home counts.
 */
function absoluteHomeRelative(location: string): string | undefined {
	const m = /^\/(?:var\/)?home\/[^/]+(?:\/(.*))?$/.exec(location) ?? /^\/root(?:\/(.*))?$/.exec(location);
	return m ? (m[1] ?? '') : undefined;
}

function homeRelative(fs: FilesystemEntry): string | undefined {
	if (fs.kind === 'home-path') return fs.location.slice(2);
	if (fs.kind === 'absolute') return absoluteHomeRelative(fs.location);
	if (fs.kind === 'xdg' && fs.xdgDir && fs.xdgDir in XDG_HOME) {
		return [XDG_HOME[fs.xdgDir], fs.subpath].filter(Boolean).join('/');
	}
	return undefined;
}

function sensitivity(fs: FilesystemEntry): 'escape' | 'secret' | undefined {
	const home = homeRelative(fs);
	if (home !== undefined) {
		if (SECRET_HOME_PATHS.some((s) => overlaps(home, s))) return 'secret';
		if (ESCAPE_HOME_PATHS.some((s) => overlaps(home, s))) return 'escape';
	}
	if (fs.kind === 'absolute' && ESCAPE_ABSOLUTE_PATHS.some((s) => overlaps(fs.location, s))) {
		return 'escape';
	}
	return undefined;
}

const ACCESS_LABEL: Record<FilesystemEntry['mode'], string> = {
	'read-only': 'read-only',
	'read-write': 'read/write',
	create: 'read/write, created if missing',
	none: 'removed'
};

const downgrade = (s: Severity): Severity => (s === 'high' ? 'warning' : 'notice');

function describeFilesystem(fs: FilesystemEntry): Omit<PermissionItem, 'id' | 'token'> {
	const ro = fs.mode === 'read-only';
	const sensitive = fs.removed ? undefined : sensitivity(fs);
	const base = (label: string, detail: string, severity: Severity, mono = false) => {
		let tier: Severity = fs.removed ? 'notice' : ro ? downgrade(severity) : severity;
		if (sensitive === 'secret') {
			tier = 'high';
			detail += ' Contains private keys.';
		} else if (sensitive === 'escape' && !ro) {
			tier = 'high';
			detail += ' Write access here allows running code outside the sandbox.';
		}
		return {
			label,
			detail,
			severity: tier,
			...(mono ? { mono } : {}),
			...(fs.removed ? { removed: true } : { access: ACCESS_LABEL[fs.mode] }),
			...(fs.problem ? { problem: fs.problem } : {})
		};
	};
	switch (fs.kind) {
		case 'host':
			return base(
				'All host files',
				'Everything but system directories: /home, /media, /opt, /srv, /run/media and host OS files in /run/host.',
				'high'
			);
		case 'host-root':
			return base('Complete host filesystem', 'The whole host root filesystem, at /run/host/root.', 'high');
		case 'host-os':
			return base('Host operating system', 'Host /usr, /bin, /lib and related directories, at /run/host.', 'high');
		case 'host-etc':
			return base('Host system configuration', 'Host /etc, at /run/host/etc.', 'high');
		case 'host-reset':
			return { ...base('Host access reset', 'Removes all host filesystem access granted below.', 'notice'), removed: true };
		case 'home':
			return base('Home folder', 'Your entire home directory.', 'high');
		case 'xdg': {
			const dir = XDG_DIRECTORIES[fs.xdgDir ?? ''] ?? fs.xdgDir ?? '';
			const isUserDir = !dir.startsWith('~') && !dir.startsWith('$');
			const label = fs.subpath ? `${dir}/${fs.subpath}` : isUserDir ? `${dir} folder` : dir;
			const whole = !fs.subpath && (fs.xdgDir === 'xdg-config' || fs.xdgDir === 'xdg-data');
			const location = `${fs.xdgDir}${fs.subpath ? `/${fs.subpath}` : ''}`;
			const detail = whole
				? `All apps’ ${fs.xdgDir === 'xdg-config' ? 'settings' : 'data'} (${location}).`
				: fs.xdgDir === 'xdg-run'
					? `An entry in the user runtime directory (${location}).`
					: `${location}.`;
			return base(label, detail, whole ? 'high' : 'notice', !isUserDir || !!fs.subpath);
		}
		case 'home-path':
			return base(fs.location, 'A location in your home directory.', 'warning', true);
		case 'absolute': {
			// The home directories of all users (or a directory containing them): at least as
			// broad as "home", including ~/.ssh and the paths that allow escaping the sandbox.
			if (HOME_ROOTS.some((h) => h === fs.location || h.startsWith(`${fs.location}/`))) {
				return base(fs.location, 'The home directories of all users, including yours.', 'high', true);
			}
			if (absoluteHomeRelative(fs.location) === '') {
				return base(fs.location, 'A user’s entire home directory.', 'high', true);
			}
			return base(fs.location, 'A location on the host.', 'warning', true);
		}
		case 'invalid':
			return base(fs.token, 'Not a valid filesystem location.', 'notice', true);
	}
}

// Session bus names whose talk/own access amounts to running code on the host.
const HOST_ESCAPE_NAMES: Record<string, string> = {
	'org.freedesktop.Flatpak': 'Can run commands on the host outside the sandbox (flatpak-spawn --host).',
	'org.freedesktop.systemd1': 'Can start services on the host outside the sandbox.'
};

// Session bus services holding stored passwords.
const SECRET_NAMES = ['org.freedesktop.secrets', 'org.gnome.keyring', 'org.kde.kwalletd5', 'org.kde.kwalletd6'];

const POLICY_DETAIL: Record<string, string> = {
	own: 'can own this name and talk to it',
	talk: 'can talk to this service',
	see: 'can see this name but not talk to it',
	none: 'no access'
};

/**
 * Whether a policy name ("org.foo.Bar", or "org.foo.*") covers `target`. Like xdg-dbus-proxy,
 * "org.foo.*" matches "org.foo" itself as well as "org.foo.bar" and deeper names.
 */
function coversName(pattern: string, target: string): boolean {
	if (pattern.endsWith('.*')) {
		const prefix = pattern.slice(0, -2);
		return target === prefix || target.startsWith(`${prefix}.`);
	}
	return pattern === target;
}

function describeBusName(bus: 'session' | 'system' | 'a11y', b: BusName): Omit<PermissionItem, 'id'> {
	const busLabel = { session: 'Session bus', system: 'System bus', a11y: 'Accessibility bus' }[bus];
	const granting = b.policy === 'talk' || b.policy === 'own';
	let severity: Severity = 'notice';
	let detail = `${busLabel}: ${POLICY_DETAIL[b.policy ?? ''] ?? `policy “${b.value}”`}.`;
	if (granting && bus === 'session') {
		const escape = Object.keys(HOST_ESCAPE_NAMES).find((n) => coversName(b.name, n));
		if (escape) {
			severity = 'high';
			detail = `${busLabel}: ${HOST_ESCAPE_NAMES[escape]}`;
		} else if (SECRET_NAMES.some((n) => coversName(b.name, n))) {
			severity = 'warning';
			detail = `${busLabel}: can read stored passwords (secret service).`;
		}
	} else if (granting && bus === 'system') {
		severity = 'warning';
	}
	return {
		label: b.name,
		mono: true,
		detail,
		token: `${b.name}=${b.value}`,
		severity,
		...(b.policy ? { access: b.policy } : {}),
		...(b.policy === 'none' ? { removed: true } : {}),
		...(b.problem ? { problem: b.problem } : {})
	};
}

const USB_CLASSES: Record<string, string> = {
	'01': 'audio',
	'02': 'communications',
	'03': 'HID (keyboards, mice, controllers)',
	'05': 'physical',
	'06': 'imaging',
	'07': 'printers',
	'08': 'mass storage',
	'09': 'hubs',
	'0a': 'CDC data',
	'0b': 'smart cards',
	'0d': 'content security',
	'0e': 'video',
	'0f': 'personal healthcare',
	'10': 'audio/video',
	'11': 'billboard',
	'12': 'USB-C bridge',
	dc: 'diagnostic',
	e0: 'wireless (e.g. Bluetooth)',
	ef: 'miscellaneous',
	fe: 'application specific',
	ff: 'vendor specific'
};

/** Human-readable USB query, e.g. "Vendor 0fd9, product 0063". */
export function describeUsbQuery(q: UsbQuery): string {
	if (q.problem) return q.raw;
	const parts = q.rules.map((r) => {
		switch (r.type) {
			case 'all':
				return 'Any USB device';
			case 'vendor':
				return `vendor ${r.id}`;
			case 'product':
				return `product ${r.id}`;
			case 'class': {
				const name = USB_CLASSES[r.class];
				const cls = name ? `class ${r.class}: ${name}` : `class ${r.class}`;
				return r.subclass ? `${cls}, subclass ${r.subclass}` : cls;
			}
		}
	});
	const text = parts.join(', ');
	return text.charAt(0).toUpperCase() + text.slice(1);
}

/** Describes all permissions of the metadata, grouped into sections. */
export function describePermissions(meta: FlatpakMetadata): PermissionSummary {
	const items = new Map<SectionId, PermissionItem[]>();
	let seq = 0;
	const add = (section: SectionId, item: Omit<PermissionItem, 'id'>) => {
		let list = items.get(section);
		if (!list) items.set(section, (list = []));
		list.push({ id: `${section}-${seq++}`, ...item });
	};
	const ctx = meta.context;

	const effect = (list: Permission[], name: string) => {
		const p = list.find((x) => x.name === name);
		return p ? permissionEffect(p) : 'never';
	};
	const hasWayland = effect(ctx.sockets, 'wayland') !== 'never';

	// An unconditional fallback-x11 limits X11 to sessions without Wayland, also when x11 itself is
	// listed (flatpak_context_compute_allowed_sockets): x11 becomes conditional on !has-wayland.
	// Before flatpak 1.19 "has Wayland" meant "granted and available", so without Wayland access
	// X11 stays always on there.
	const fallbackX11 = ctx.sockets.some((p) => p.name === 'fallback-x11' && p.state === 'allowed');
	const effectiveSocket = (p: Permission): Permission => {
		if (!fallbackX11 || p.name !== 'x11' || p.state === 'denied') return p;
		const outsideWayland = { name: 'has-wayland', negated: true };
		const has = p.conditions.some((c) => c.name === outsideWayland.name && c.negated);
		return { ...p, state: 'conditional', conditions: has ? p.conditions : [...p.conditions, outsideWayland] };
	};

	const addPermissions = (key: string, list: Permission[], table: Record<string, Known>, fallback: SectionId) => {
		for (const listed of list) {
			const p = key === 'sockets' ? effectiveSocket(listed) : listed;
			const known = table[p.name];
			// The token as written in the metadata.
			const token =
				listed.state === 'denied'
					? `${key}=!${listed.name}`
					: listed.state === 'conditional'
						? `${key}=${listed.conditions.map((c) => `if:${listed.name}:${c.negated ? '!' : ''}${c.name}`).join(';')}`
						: `${key}=${listed.name}`;
			const item: Omit<PermissionItem, 'id'> = known
				? { label: known.label, detail: known.detail, token, severity: known.severity }
				: {
						label: p.name,
						mono: true,
						detail: 'Not known to flatpak; ignored.',
						token,
						severity: 'notice'
					};
			const eff = permissionEffect(p);
			if (p.state === 'denied') {
				item.removed = true;
				item.severity = 'notice';
			} else if (p.state === 'conditional') {
				if (eff === 'never') {
					item.condition = 'never granted: no condition can be true';
					item.severity = 'notice';
				} else if (eff === 'sometimes') {
					// flatpak ORs the conditions; only the ones that depend on the session matter here.
					const runtime = p.conditions.filter((c) => c.name === 'has-wayland' || c.name === 'has-usb-portal');
					item.condition = `only ${runtime.map(describeCondition).join(' or ')}`;
					if (p.name === 'x11' && runtime.every((c) => c.name === 'has-wayland' && c.negated)) {
						item.severity = 'notice';
					}
				}
			}
			if (p !== listed) {
				// x11 limited by fallback-x11.
				if (hasWayland) {
					item.detail = `${known?.detail ?? ''} Listed with fallback-x11, so only used when no Wayland session is available.`;
				} else {
					item.severity = 'warning';
					item.condition = 'only outside a Wayland session on flatpak 1.19 and newer, always on older versions';
					item.detail =
						'No Wayland access: with fallback-x11, flatpak 1.19 and newer grant X11 only outside a Wayland session (the app may not display in one), older versions always use the legacy X11 system, where clients can capture other windows and read their input.';
				}
			}
			if (p.name === 'x11' && eff === 'always' && key === 'sockets') {
				if (hasWayland) {
					item.severity = 'notice';
					item.detail = 'Granted in addition to Wayland: X11 clients can capture other X11 windows and read their input.';
				} else {
					item.detail = 'No Wayland access, so the app always uses the legacy X11 system, where clients can capture other windows and read their input.';
				}
			}
			add(known?.section ?? fallback, item);
		}
	};

	addPermissions('shared', ctx.shared, SHARED, 'network');
	addPermissions('sockets', ctx.sockets, SOCKETS, 'other');
	addPermissions('devices', ctx.devices, DEVICES, 'devices');
	addPermissions('features', ctx.features, FEATURES, 'features');

	for (const fs of ctx.filesystems) {
		add('filesystem', { token: `filesystems=${fs.token}`, ...describeFilesystem(fs) });
	}
	for (const p of ctx.persistent) {
		add('filesystem', {
			label: `~/${p.replace(/^\/+/, '')}`,
			mono: true,
			detail: 'Persistent: stored in the app’s own data folder (~/.var/app/…), not in your home.',
			token: `persistent=${p}`,
			severity: 'notice'
		});
	}

	for (const b of meta.sessionBus) add('dbus', describeBusName('session', b));
	for (const b of meta.systemBus) add('dbus', describeBusName('system', b));
	for (const b of meta.a11yBus) add('dbus', describeBusName('a11y', b));

	for (const q of meta.usb.enumerable) {
		const all = q.rules.some((r) => r.type === 'all');
		add('usb', {
			label: describeUsbQuery(q),
			detail: 'Can be listed and requested through the USB portal.',
			token: `enumerable-devices=${q.raw}`,
			severity: all ? 'warning' : 'notice',
			...(q.problem ? { problem: q.problem } : {})
		});
	}
	for (const q of meta.usb.hidden) {
		add('usb', {
			label: describeUsbQuery(q),
			detail: 'Hidden from the app, even if listed above.',
			token: `hidden-devices=${q.raw}`,
			severity: 'notice',
			removed: true,
			...(q.problem ? { problem: q.problem } : {})
		});
	}

	for (const v of meta.environment) {
		add('environment', {
			label: v.name,
			mono: true,
			...(v.value === ''
				? { detail: 'Set to an empty value (older flatpak versions unset it).' }
				: { detail: v.value, detailMono: true }),
			token: `${v.name}=${v.value}`,
			severity: 'notice'
		});
	}
	for (const name of ctx.unsetEnvironment) {
		add('environment', {
			label: name,
			mono: true,
			detail: 'Unset in the sandbox.',
			token: `unset-environment=${name}`,
			severity: 'notice',
			removed: true
		});
	}

	for (const p of meta.policies) {
		add('other', {
			label: `${p.subsystem}.${p.key}`,
			mono: true,
			detail: `Policy for the ${p.subsystem} subsystem: ${p.values.join(', ') || '(empty)'}.`,
			token: `[Policy ${p.subsystem}] ${p.key}=${p.values.join(';')}`,
			severity: 'notice'
		});
	}
	if (meta.dconf) {
		const { paths, migratePath } = meta.dconf;
		add('other', {
			label: 'dconf settings',
			detail: [
				paths ? `Paths: ${paths}` : '',
				migratePath ? `migrates settings from ${migratePath}` : ''
			]
				.filter(Boolean)
				.join('; '),
			token: '[X-DConf]',
			severity: 'notice'
		});
	}
	for (const token of ctx.invalid) {
		add('other', {
			label: token,
			mono: true,
			detail: 'Invalid permission syntax.',
			token,
			severity: 'notice',
			problem: 'flatpak rejects metadata with this entry'
		});
	}

	// Broadest access first within a section, removed entries last; stable otherwise.
	const rank = (i: PermissionItem) => (i.removed ? 3 : { high: 0, warning: 1, notice: 2 }[i.severity]);
	const order = Object.keys(SECTION_TITLES) as SectionId[];
	const sections = order
		.filter((id) => items.has(id))
		.map((id) => ({
			id,
			title: SECTION_TITLES[id],
			items: [...(items.get(id) ?? [])].sort((a, b) => rank(a) - rank(b))
		}));
	const counts: Record<Severity, number> = { high: 0, warning: 0, notice: 0 };
	let total = 0;
	for (const s of sections) {
		for (const item of s.items) {
			total++;
			if (!item.removed) counts[item.severity]++;
		}
	}
	return { sections, counts, total };
}
