/**
 * Typed interpretation of flatpak metadata (the org.flatpak.metadata label of
 * a flatpak OCI image, the same file as `metadata` in a deploy directory).
 * See flatpak-metadata(5). Semantics follow flatpak's own parsers:
 * common/flatpak-context.c (permissions), common/flatpak-dir.c (extensions,
 * extra data) and app/flatpak-builtins-build-export.c (extra data sources).
 *
 * Pure TypeScript without app imports so it runs under `deno test`.
 */
import { parseKeyFile, type KeyFile, type KeyFileError } from './keyfile.ts';

export const GROUP_APPLICATION = 'Application';
export const GROUP_RUNTIME = 'Runtime';
export const GROUP_CONTEXT = 'Context';
export const GROUP_SESSION_BUS_POLICY = 'Session Bus Policy';
export const GROUP_SYSTEM_BUS_POLICY = 'System Bus Policy';
export const GROUP_A11Y_BUS_POLICY = 'Accessibility Bus Policy';
export const GROUP_ENVIRONMENT = 'Environment';
export const GROUP_EXTENSION_OF = 'ExtensionOf';
export const GROUP_EXTRA_DATA = 'Extra Data';
export const GROUP_USB_DEVICES = 'USB Devices';
export const GROUP_DCONF = 'X-DConf';
export const GROUP_PREFIX_EXTENSION = 'Extension ';
export const GROUP_PREFIX_POLICY = 'Policy ';

export type FlatpakKind = 'app' | 'runtime';

// ---------------------------------------------------------------------------
// Refs

/** "id/arch/branch" as used by the runtime and sdk keys ("partial ref"). */
export interface PartialRef {
	id: string;
	arch: string;
	branch: string;
}

/** A full ref: "app/id/arch/branch" or "runtime/id/arch/branch". */
export interface FullRef extends PartialRef {
	kind: FlatpakKind;
}

/** Parses "id/arch/branch"; arch and branch may be missing (''). */
export function parsePartialRef(pref: string): PartialRef | undefined {
	const parts = pref.trim().split('/');
	if (parts.length > 3 || !parts[0]) return undefined;
	return { id: parts[0], arch: parts[1] ?? '', branch: parts[2] ?? '' };
}

/** Parses "app/id/arch/branch" or "runtime/id/arch/branch". */
export function parseFullRef(ref: string): FullRef | undefined {
	const parts = ref.trim().split('/');
	if (parts.length !== 4 || (parts[0] !== 'app' && parts[0] !== 'runtime') || !parts[1]) {
		return undefined;
	}
	return { kind: parts[0], id: parts[1], arch: parts[2], branch: parts[3] };
}

// ---------------------------------------------------------------------------
// [Context] permission lists: shared, sockets, devices, features

/** A condition of a conditional permission, e.g. `!has-wayland`. */
export interface Condition {
	name: string;
	negated: boolean;
}

/**
 * - allowed: granted unconditionally
 * - conditional: granted when any of `conditions` holds (`if:name:cond`)
 * - denied: explicitly removed (`!name`), e.g. revoking what the runtime or
 *   a lower layer grants
 */
export type PermissionState = 'allowed' | 'conditional' | 'denied';

export interface Permission {
	name: string;
	state: PermissionState;
	/** Only for `conditional`; sorted, unique. */
	conditions: Condition[];
}

/** The outcome of a condition on a current flatpak. */
export type ConditionOutcome = 'true' | 'false' | 'runtime';

/** Conditions that are always true in current flatpak versions. */
const ALWAYS_TRUE_CONDITIONS = new Set(['true', 'has-input-device', 'has-usb-device']);
/** Conditions flatpak evaluates when the app starts. */
const RUNTIME_CONDITIONS = new Set(['has-wayland', 'has-usb-portal']);

export function parseCondition(text: string): Condition {
	return text.startsWith('!')
		? { name: text.slice(1), negated: true }
		: { name: text, negated: false };
}

/**
 * How a condition evaluates (flatpak_permission_compute_allowed): unknown
 * conditions are never satisfied, negated or not.
 */
export function conditionOutcome(c: Condition): ConditionOutcome {
	if (ALWAYS_TRUE_CONDITIONS.has(c.name)) return c.negated ? 'false' : 'true';
	if (c.name === 'false') return c.negated ? 'true' : 'false';
	if (RUNTIME_CONDITIONS.has(c.name)) return 'runtime';
	return 'false';
}

/** Whether a permission is in effect: always, depending on the session, or never. */
export function permissionEffect(p: Permission): 'always' | 'sometimes' | 'never' {
	if (p.state === 'allowed') return 'always';
	if (p.state === 'denied') return 'never';
	const outcomes = p.conditions.map(conditionOutcome);
	if (outcomes.includes('true')) return 'always';
	if (outcomes.includes('runtime')) return 'sometimes';
	return 'never';
}

/**
 * Parses a permission list like flatpak_permissions_from_strv: `name`,
 * `!name` and `if:name:condition`, applied in order. An unconditional `name`
 * followed by conditionals of the same name is the backwards-compatibility
 * form ("x11;if:x11:!has-wayland") and becomes conditional. Tokens flatpak
 * rejects (flatpak then refuses the whole metadata) end up in `invalid`.
 */
export function parsePermissionList(tokens: string[]): {
	permissions: Permission[];
	invalid: string[];
} {
	interface Acc {
		allowed: boolean;
		reset: boolean;
		conditions: string[];
		compat?: { reset: boolean; conditions: string[] };
	}
	const acc = new Map<string, Acc>();
	const invalid: string[] = [];

	for (const token of tokens) {
		const parts = splitN(token, ':', 3);
		let name: string;
		let negated = false;
		let condition: string | undefined;
		if (parts[0] === 'if') {
			if (parts.length !== 3) {
				invalid.push(token);
				continue;
			}
			name = parts[1];
			condition = parts[2];
		} else {
			if (parts.length !== 1) {
				invalid.push(token);
				continue;
			}
			negated = token.startsWith('!');
			name = negated ? token.slice(1) : token;
		}

		let p = acc.get(name);
		if (!p) acc.set(name, (p = { allowed: false, reset: false, conditions: [] }));

		if (condition === undefined) {
			if (negated) {
				p.allowed = false;
				p.reset = true;
			} else {
				// Possibly the backwards-compat form of a following conditional.
				p.compat = { reset: p.reset, conditions: p.conditions };
				p.conditions = [];
				p.allowed = true;
				p.reset = true;
			}
		} else {
			if (p.compat) {
				p.allowed = false;
				p.reset = p.compat.reset;
				p.conditions = p.compat.conditions;
				p.compat = undefined;
			}
			if (!p.conditions.includes(condition)) p.conditions = [...p.conditions, condition].sort();
		}
	}

	const permissions = [...acc].map(([name, p]): Permission => {
		if (p.allowed) return { name, state: 'allowed', conditions: [] };
		if (p.conditions.length > 0) {
			return { name, state: 'conditional', conditions: p.conditions.map(parseCondition) };
		}
		return { name, state: 'denied', conditions: [] };
	});
	return { permissions, invalid };
}

/** g_strsplit(text, sep, max): at most `max` pieces, the last one keeps the rest. */
function splitN(text: string, sep: string, max: number): string[] {
	const parts: string[] = [];
	let rest = text;
	while (parts.length < max - 1) {
		const i = rest.indexOf(sep);
		if (i < 0) break;
		parts.push(rest.slice(0, i));
		rest = rest.slice(i + sep.length);
	}
	parts.push(rest);
	return parts;
}

// ---------------------------------------------------------------------------
// [Context] filesystems

export type FilesystemMode = 'read-only' | 'read-write' | 'create' | 'none';

/**
 * - host, host-os, host-etc, host-root, home: the special locations
 * - host-reset: `!host:reset`, removes all host access granted below
 * - xdg: an XDG directory (xdg-download, xdg-config/foo, xdg-run/foo, ...)
 * - home-path: `~/path`; absolute: `/path`
 * - invalid: rejected or ignored by flatpak (see `problem`)
 */
export type FilesystemKind =
	| 'host'
	| 'host-os'
	| 'host-etc'
	| 'host-root'
	| 'host-reset'
	| 'home'
	| 'xdg'
	| 'home-path'
	| 'absolute'
	| 'invalid';

export interface FilesystemEntry {
	/** The list element as written, e.g. "!~/Games:ro". */
	token: string;
	/** Normalized location, e.g. "home", "~/Games", "xdg-config/foo". */
	location: string;
	kind: FilesystemKind;
	/** `none` for removed entries. */
	mode: FilesystemMode;
	/** Negated (`!location`): access granted at a lower layer is removed. */
	removed: boolean;
	/** For kind xdg: the directory ("xdg-download") and the path below it. */
	xdgDir?: string;
	subpath?: string;
	/** Why flatpak rejects or ignores this entry, or ignores part of it. */
	problem?: string;
}

const SPECIAL_FILESYSTEMS = new Set(['home', 'host', 'host-etc', 'host-os', 'host-reset', 'host-root']);

/** XDG directories flatpak knows, with a human-readable name. */
export const XDG_DIRECTORIES: Record<string, string> = {
	'xdg-desktop': 'Desktop',
	'xdg-documents': 'Documents',
	'xdg-download': 'Downloads',
	'xdg-music': 'Music',
	'xdg-pictures': 'Pictures',
	'xdg-public-share': 'Public share',
	'xdg-templates': 'Templates',
	'xdg-videos': 'Videos',
	'xdg-data': '~/.local/share',
	'xdg-cache': '~/.cache',
	'xdg-config': '~/.config',
	'xdg-run': '$XDG_RUNTIME_DIR'
};

/** Parses one `filesystems` element like flatpak_context_parse_filesystem. */
export function parseFilesystem(token: string): FilesystemEntry {
	const removed = token.startsWith('!');
	const body = removed ? token.slice(1) : token;

	// parse_filesystem_flags: the location ends at the first unescaped ':'.
	let location = '';
	let i = 0;
	for (; i < body.length && body[i] !== ':'; i++) {
		if (body[i] === '\\') {
			i++;
			if (i < body.length) location += body[i];
		} else {
			location += body[i];
		}
	}
	const suffix = i < body.length ? body.slice(i + 1) : undefined;

	let mode: FilesystemMode = removed ? 'none' : 'read-write';
	let reset = false;
	let problem: string | undefined;
	const entry = (kind: FilesystemKind, loc = location): FilesystemEntry => ({
		token,
		location: loc,
		kind,
		mode,
		removed,
		...(problem ? { problem } : {})
	});
	const invalid = (why: string) => {
		problem = why;
		return entry('invalid');
	};

	if (location === 'host-reset') {
		if (!removed) return invalid('host-reset is only valid as “!host-reset”');
		if (suffix !== undefined) return invalid('host-reset cannot have a suffix');
	}
	if (suffix !== undefined) {
		if (suffix === 'ro') mode = 'read-only';
		else if (suffix === 'rw') mode = 'read-write';
		else if (suffix === 'create') mode = 'create';
		else if (suffix === 'reset') reset = true;
		else if (suffix !== '') problem = `Unknown suffix “:${suffix}” is ignored`;
		if (removed && mode !== 'none') {
			problem = `Suffix “:${suffix}” does not apply to a removed location`;
			mode = 'none';
		}
		if (reset) {
			if (!removed) return invalid('“:reset” only applies to a removed location (“!host:reset”)');
			if (location !== 'host') return invalid('“:reset” only applies to “!host:reset”');
			location = 'host-reset';
		}
	}

	if (location.endsWith('/..') || location.includes('/../')) {
		return invalid('Locations must not contain “..”');
	}
	if (location.includes('/')) {
		location = location.replace(/\/(?:\.?\/)+/g, '/');
		while (/.\/\.?$/.test(location) || location.endsWith('/.')) {
			location = location.replace(/\/\.?$/, '');
		}
		if (location === '/') return invalid('“/” is not available, use “host” instead');
	}

	if (SPECIAL_FILESYSTEMS.has(location)) {
		return entry(location as FilesystemKind);
	}
	if (location === '~') return entry('home', 'home');
	if (location.startsWith('home/')) location = `~/${location.slice(5)}`;
	if (location.startsWith('~/')) return entry('home-path');
	if (location.startsWith('/')) return entry('absolute');

	const slash = location.indexOf('/');
	const dir = slash < 0 ? location : location.slice(0, slash);
	const sub = slash < 0 ? '' : location.slice(slash + 1).replace(/^\/+/, '');
	if (dir in XDG_DIRECTORIES && (dir !== 'xdg-run' || sub !== '')) {
		return { ...entry('xdg'), xdgDir: dir, subpath: sub };
	}
	return invalid(
		dir === 'xdg-run' ? '“xdg-run” needs a subdirectory' : `Unknown location “${location}”, ignored by flatpak`
	);
}

// ---------------------------------------------------------------------------
// D-Bus policies

/** none < see < talk < own. */
export type BusPolicy = 'none' | 'see' | 'talk' | 'own';
export const BUS_POLICIES: readonly BusPolicy[] = ['none', 'see', 'talk', 'own'];

export interface BusName {
	/** A well-known name or a prefix ending in ".*". */
	name: string;
	/** Unknown policy values are ignored by flatpak and reported in `problem`. */
	policy: BusPolicy | undefined;
	value: string;
	problem?: string;
}

const DBUS_ELEMENT = '[A-Za-z_-][A-Za-z0-9_-]*';
const DBUS_NAME = new RegExp(`^${DBUS_ELEMENT}(?:\\.${DBUS_ELEMENT})+$`);

/** flatpak_verify_dbus_name: a well-known bus name, optionally ending in ".*". */
export function isValidBusName(name: string): boolean {
	const base = name.endsWith('.*') ? name.slice(0, -2) : name;
	return base.length <= 255 && DBUS_NAME.test(base);
}

function parseBusPolicy(file: KeyFile, group: string): BusName[] {
	return file.keys(group).map((name) => {
		const value = file.string(group, name) ?? '';
		const policy = (BUS_POLICIES as readonly string[]).includes(value)
			? (value as BusPolicy)
			: undefined;
		const problem = !isValidBusName(name)
			? 'Invalid D-Bus name; flatpak rejects the metadata'
			: policy === undefined
				? `Unknown policy “${value}”, ignored`
				: undefined;
		return { name, policy, value, ...(problem ? { problem } : {}) };
	});
}

// ---------------------------------------------------------------------------
// [USB Devices]

export type UsbRule =
	| { type: 'all' }
	| { type: 'vendor'; id: string }
	| { type: 'product'; id: string }
	| { type: 'class'; class: string; subclass: string | undefined };

export interface UsbQuery {
	/** As written, e.g. "vnd:0fd9+dev:0063". */
	raw: string;
	rules: UsbRule[];
	/** Why flatpak rejects the query (and the metadata). */
	problem?: string;
}

const HEX4 = /^[0-9a-fA-F]{4}$/;
const HEX2 = /^[0-9a-fA-F]{2}$/;

/** Parses a USB query like flatpak_usb_parse_usb ("all", "cls:03:*", "vnd:XXXX+dev:XXXX"). */
export function parseUsbQuery(raw: string): UsbQuery {
	const rules: UsbRule[] = [];
	const fail = (problem: string): UsbQuery => ({ raw, rules, problem });
	if (raw === '') return fail('Empty USB query');
	for (const part of raw.split('+')) {
		const f = part.split(':');
		if (f.length > 3) return fail('USB queries must be in the form TYPE:DATA');
		switch (f[0]) {
			case 'all':
				if (f.length !== 1) return fail('“all” must not have data');
				rules.push({ type: 'all' });
				break;
			case 'cls':
				if (f.length < 3 || !HEX2.test(f[1]) || (f[2] !== '*' && !HEX2.test(f[2]))) {
					return fail('“cls” must be CLASS:SUBCLASS or CLASS:* (hex)');
				}
				rules.push({ type: 'class', class: f[1].toLowerCase(), subclass: f[2] === '*' ? undefined : f[2].toLowerCase() });
				break;
			case 'vnd':
			case 'dev':
				if (f.length !== 2 || !HEX4.test(f[1])) {
					return fail(`“${f[0]}” needs a 4-digit hexadecimal id`);
				}
				rules.push({ type: f[0] === 'vnd' ? 'vendor' : 'product', id: f[1].toLowerCase() });
				break;
			default:
				return fail(`Unknown USB query rule “${f[0]}”`);
		}
	}
	const types = rules.map((r) => r.type);
	if (new Set(types).size !== types.length) return fail('Repeated rule type');
	if (types.includes('all') && types.length > 1) return fail('“all” must not be combined');
	if (types.includes('product') && !types.includes('vendor')) {
		return fail('“dev” needs a vendor (“vnd”) too');
	}
	return { raw, rules };
}

// ---------------------------------------------------------------------------
// [Extension NAME]

/**
 * - auto: downloaded together with the app/runtime when the remote has it
 * - conditional: downloaded only if one of `download-if` holds on the client
 * - manual: never downloaded automatically (`no-autodownload=true`)
 * - debug: *.Debug extensions, only updated when installed by hand
 */
export type ExtensionDownloadMode = 'auto' | 'conditional' | 'manual' | 'debug';

export interface ExtensionPoint {
	/** Group name, e.g. "Extension org.freedesktop.Platform.GL@1.4". */
	group: string;
	/** Extension id without the tag, e.g. "org.freedesktop.Platform.GL". */
	name: string;
	/** The "@tag" part of the group name, if any. */
	tag?: string;
	/** Mount point below /app (apps) or /usr (runtimes). Mandatory for flatpak. */
	directory?: string;
	/** Branches looked up (`versions`, else `version`); undefined = the parent's branch. */
	versions?: string[];
	/** Also matches extensions named "<name>.*", mounted in subdirectories. */
	subdirectories: boolean;
	noAutodownload: boolean;
	downloadIf: string[];
	enableIf: string[];
	autopruneUnless: string[];
	autodelete: boolean;
	localeSubset: boolean;
	addLdPath?: string;
	mergeDirs: string[];
	subdirectorySuffix?: string;
	collectionId?: string;
	/** Derived install behaviour. */
	download: ExtensionDownloadMode;
	/** Only the configured locales are downloaded (*.Locale or locale-subset). */
	partial: boolean;
	/** Uninstalled together with the parent (autodelete, *.Debug, locale subsets). */
	removedWithParent: boolean;
}

function splitConditions(value: string | undefined): string[] {
	return (value ?? '').split(';').filter(Boolean);
}

function parseExtensionPoint(file: KeyFile, group: string): ExtensionPoint {
	const tagged = group.slice(GROUP_PREFIX_EXTENSION.length);
	const at = tagged.indexOf('@');
	const name = at < 0 ? tagged : tagged.slice(0, at);
	const tag = at < 0 ? undefined : tagged.slice(at + 1);
	const bool = (key: string) => file.boolean(group, key) ?? false;
	const str = (key: string) => file.string(group, key);

	const version = str('version');
	const versions = file.stringList(group, 'versions') ?? (version !== undefined ? [version] : undefined);
	const noAutodownload = bool('no-autodownload');
	const downloadIf = splitConditions(str('download-if'));
	const localeSubset = bool('locale-subset');
	const autodelete = bool('autodelete');

	// flatpak-dir.c add_related(): download-if, when set, decides alone (it
	// overrides no-autodownload); *.Debug is never fetched for new installs.
	const debug = name.endsWith('.Debug');
	const download: ExtensionDownloadMode = debug
		? 'debug'
		: downloadIf.length > 0
			? 'conditional'
			: noAutodownload
				? 'manual'
				: 'auto';
	const partial = localeSubset || name.endsWith('.Locale');

	return {
		group,
		name,
		...(tag !== undefined ? { tag } : {}),
		...(str('directory') !== undefined ? { directory: str('directory') } : {}),
		...(versions ? { versions } : {}),
		subdirectories: bool('subdirectories'),
		noAutodownload,
		downloadIf,
		enableIf: splitConditions(str('enable-if')),
		autopruneUnless: splitConditions(str('autoprune-unless')),
		autodelete,
		localeSubset,
		...(str('add-ld-path') !== undefined ? { addLdPath: str('add-ld-path') } : {}),
		mergeDirs: file.stringList(group, 'merge-dirs') ?? [],
		...(str('subdirectory-suffix') !== undefined
			? { subdirectorySuffix: str('subdirectory-suffix') }
			: {}),
		...(str('collection-id') !== undefined ? { collectionId: str('collection-id') } : {}),
		download,
		partial,
		removedWithParent: autodelete || debug || partial
	};
}

/** Human-readable description of a download-if / enable-if condition. */
export function describeExtensionCondition(condition: string): string {
	if (condition === 'active-gl-driver') return 'the extension matches the active GL driver';
	if (condition === 'active-gtk-theme') return 'the extension matches the current GTK theme';
	if (condition === 'have-intel-gpu') return 'an Intel GPU is present (i915 loaded)';
	if (condition.startsWith('have-kernel-module-')) {
		return `the kernel module “${condition.slice('have-kernel-module-'.length)}” is loaded`;
	}
	if (condition.startsWith('on-xdg-desktop-')) {
		return `running on the “${condition.slice('on-xdg-desktop-'.length)}” desktop`;
	}
	return `unknown condition “${condition}” (never true)`;
}

// ---------------------------------------------------------------------------
// [ExtensionOf]

export interface ExtensionOf {
	/** The app or runtime this extension belongs to, e.g. "runtime/org.x.Platform/x86_64/1". */
	ref?: string;
	/** The runtime the extension runs inside. */
	runtime?: string;
	priority?: number;
	/** Which "@tag" extension point of the parent it is mounted at. */
	tag?: string;
}

// ---------------------------------------------------------------------------
// [Extra Data]

export interface ExtraDataSource {
	/** Key suffix grouping the entries: "" for uri/name/..., "1" for uri1/name1/... */
	suffix: string;
	/** File name in /app/extra (explicit, or the last part of the URI). */
	name: string;
	uri: string;
	/** Normalized URL when `uri` is http(s) and may be rendered as a link. */
	httpUrl?: string;
	/** Host the file is downloaded from (with `httpUrl`). */
	host?: string;
	/** Download size in bytes. */
	size?: number;
	installedSize?: number;
	/** SHA-256 the download is verified against. */
	checksum?: string;
	/** Reasons flatpak would refuse this source (at export time). */
	problems: string[];
}

export interface ExtraData {
	/**
	 * `NoRuntime=true`: the apply_extra script runs without the runtime.
	 * flatpak defaults to mounting the runtime, so it must be installed.
	 */
	noRuntime: boolean;
	sources: ExtraDataSource[];
}

// Characters URL parsers disagree on. Browsers (WHATWG URL) read "\" as "/" in http(s) URLs,
// while GLib and curl, which flatpak downloads with, keep it in the authority: for
// "https://a.example\@b.example/x" a link opens a.example but flatpak fetches from b.example.
// Whitespace and control characters are handled differently too.
// eslint-disable-next-line no-control-regex
const AMBIGUOUS_URI_CHARS = /[\\\s\u0000-\u001f\u007f]/;

/**
 * Whether browsers and flatpak may resolve `uri` to different hosts, or it hides its host behind
 * credentials ("https://trusted.example@other.example/"): a backslash, whitespace or control
 * characters, or a user name or password.
 */
export function ambiguousUri(uri: string): boolean {
	if (AMBIGUOUS_URI_CHARS.test(uri)) return true;
	try {
		const url = new URL(uri);
		return url.username !== '' || url.password !== '';
	} catch {
		return false;
	}
}

/**
 * The URL as a string safe to use as a link target, or undefined for anything
 * other than an absolute http(s) URL (javascript:, data:, relative, ...) and for
 * URIs whose host is ambiguous (see ambiguousUri).
 */
export function safeHttpUrl(uri: string): string | undefined {
	if (!/^https?:\/\//i.test(uri) || ambiguousUri(uri)) return undefined;
	try {
		const url = new URL(uri);
		return url.protocol === 'http:' || url.protocol === 'https:' ? url.href : undefined;
	} catch {
		return undefined;
	}
}

function uriBasename(uri: string): string {
	let path = uri;
	try {
		path = new URL(uri).pathname;
	} catch {
		// keep the raw string
	}
	let end = path.length;
	while (end > 0 && path[end - 1] === '/') end--; // not /\/+$/, which is quadratic on long runs
	const base = path.slice(0, end).split('/').pop() ?? '';
	try {
		return decodeURIComponent(base);
	} catch {
		return base;
	}
}

function parseExtraData(file: KeyFile): ExtraData | undefined {
	const g = GROUP_EXTRA_DATA;
	if (!file.hasGroup(g)) return undefined;
	const sources: ExtraDataSource[] = [];
	for (const key of file.keys(g)) {
		if (!key.startsWith('uri')) continue;
		const suffix = key.slice(3);
		const uri = file.string(g, key) ?? '';
		const problems: string[] = [];
		const httpUrl = safeHttpUrl(uri);
		const host = httpUrl ? new URL(httpUrl).host : undefined;
		if (!/^https?:/.test(uri)) problems.push('Only http and https URIs are supported');
		if (ambiguousUri(uri)) {
			problems.push(
				'The URI contains a backslash, whitespace, control characters or credentials: browsers and flatpak may resolve it to different hosts'
			);
		}

		let name = file.string(g, `name${suffix}`);
		if (name === undefined || name === '') {
			name = uriBasename(uri);
			if (!name) problems.push('No file name in the URI; a name is required');
		}
		if (name.includes('/')) problems.push('The file name must not contain “/”');

		const checksum = file.string(g, `checksum${suffix}`);
		if (checksum === undefined) problems.push('Missing checksum');
		else if (!/^[0-9a-f]{64}$/.test(checksum)) problems.push('Invalid SHA-256 checksum');

		const size = file.integer(g, `size${suffix}`);
		if (size === undefined || size <= 0) problems.push('Missing or zero size');
		const installedSize = file.integer(g, `installed-size${suffix}`);

		sources.push({
			suffix,
			name,
			uri,
			...(httpUrl ? { httpUrl } : {}),
			...(host ? { host } : {}),
			...(size !== undefined ? { size } : {}),
			...(installedSize !== undefined ? { installedSize } : {}),
			...(checksum !== undefined ? { checksum } : {}),
			problems
		});
	}
	sources.sort((a, b) => a.suffix.localeCompare(b.suffix, undefined, { numeric: true }));
	return { noRuntime: file.boolean(g, 'NoRuntime') ?? false, sources };
}

// ---------------------------------------------------------------------------
// The whole file

export interface EnvironmentVariable {
	name: string;
	value: string;
}

export interface GenericPolicy {
	/** From the group name "[Policy SUBSYSTEM]". */
	subsystem: string;
	key: string;
	/** List values; a leading '!' negates. */
	values: string[];
}

export interface Context {
	shared: Permission[];
	sockets: Permission[];
	devices: Permission[];
	features: Permission[];
	filesystems: FilesystemEntry[];
	/** Home-relative paths kept in the app's own data directory. */
	persistent: string[];
	unsetEnvironment: string[];
	/** Permission tokens flatpak rejects, as "key: token". */
	invalid: string[];
}

export interface FlatpakMetadata {
	/** From the group present: [Application] = app, [Runtime] = runtime. */
	kind: FlatpakKind | undefined;
	/** The main group ("Application" or "Runtime"), if present. */
	mainGroup: string | undefined;
	/** The flatpak id ([Application] / [Runtime] name). */
	name?: string;
	runtime?: string;
	sdk?: string;
	command?: string;
	requiredFlatpak: string[];
	tags: string[];
	context: Context;
	sessionBus: BusName[];
	systemBus: BusName[];
	a11yBus: BusName[];
	environment: EnvironmentVariable[];
	policies: GenericPolicy[];
	usb: { enumerable: UsbQuery[]; hidden: UsbQuery[] };
	dconf?: { paths?: string; migratePath?: string };
	extensions: ExtensionPoint[];
	extensionOf?: ExtensionOf;
	extraData?: ExtraData;
	/** Groups this interpretation does not know. */
	unknownGroups: string[];
}

export type MetadataResult =
	| { ok: true; metadata: FlatpakMetadata; keyfile: KeyFile }
	| { ok: false; errors: KeyFileError[]; keyfile: KeyFile };

const KNOWN_GROUPS = new Set([
	GROUP_APPLICATION,
	GROUP_RUNTIME,
	GROUP_CONTEXT,
	GROUP_SESSION_BUS_POLICY,
	GROUP_SYSTEM_BUS_POLICY,
	GROUP_A11Y_BUS_POLICY,
	GROUP_ENVIRONMENT,
	GROUP_EXTENSION_OF,
	GROUP_EXTRA_DATA,
	GROUP_USB_DEVICES,
	GROUP_DCONF,
	'Instance'
]);

/**
 * Parses and interprets flatpak metadata. Fails only when the text is not a
 * valid key file (flatpak would reject it too). `kind` picks the main group
 * when both [Application] and [Runtime] exist (flatpak goes by the ref kind).
 */
export function parseMetadata(text: string, opts: { kind?: FlatpakKind } = {}): MetadataResult {
	const keyfile = parseKeyFile(text);
	if (!keyfile.valid) return { ok: false, errors: keyfile.errors, keyfile };
	return { ok: true, metadata: interpretMetadata(keyfile, opts), keyfile };
}

export function interpretMetadata(file: KeyFile, opts: { kind?: FlatpakKind } = {}): FlatpakMetadata {
	const hasApp = file.hasGroup(GROUP_APPLICATION);
	const hasRuntime = file.hasGroup(GROUP_RUNTIME);
	let kind: FlatpakKind | undefined;
	if (opts.kind === 'app' && hasApp) kind = 'app';
	else if (opts.kind === 'runtime' && hasRuntime) kind = 'runtime';
	else kind = hasApp ? 'app' : hasRuntime ? 'runtime' : undefined;
	const main = kind === 'app' ? GROUP_APPLICATION : kind === 'runtime' ? GROUP_RUNTIME : undefined;
	const mainStr = (key: string) => (main ? file.string(main, key) : undefined);

	const c = GROUP_CONTEXT;
	const invalid: string[] = [];
	const permissions = (key: string) => {
		const parsed = parsePermissionList(file.stringList(c, key) ?? []);
		invalid.push(...parsed.invalid.map((t) => `${key}: ${t}`));
		return parsed.permissions;
	};
	const context: Context = {
		shared: permissions('shared'),
		sockets: permissions('sockets'),
		devices: permissions('devices'),
		features: permissions('features'),
		filesystems: dedupeFilesystems((file.stringList(c, 'filesystems') ?? []).map(parseFilesystem)),
		persistent: file.stringList(c, 'persistent') ?? [],
		unsetEnvironment: file.stringList(c, 'unset-environment') ?? [],
		invalid
	};

	const policies: GenericPolicy[] = [];
	const extensions: ExtensionPoint[] = [];
	const unknownGroups: string[] = [];
	for (const group of file.groups()) {
		if (group.startsWith(GROUP_PREFIX_EXTENSION) && group.length > GROUP_PREFIX_EXTENSION.length) {
			extensions.push(parseExtensionPoint(file, group));
		} else if (group.startsWith(GROUP_PREFIX_POLICY)) {
			const subsystem = group.slice(GROUP_PREFIX_POLICY.length);
			for (const key of file.keys(group)) {
				policies.push({ subsystem, key, values: file.stringList(group, key) ?? [] });
			}
		} else if (!KNOWN_GROUPS.has(group)) {
			unknownGroups.push(group);
		}
	}

	const eo = GROUP_EXTENSION_OF;
	const extensionOf: ExtensionOf | undefined = file.hasGroup(eo)
		? compact({
				ref: file.string(eo, 'ref'),
				runtime: file.string(eo, 'runtime'),
				priority: file.integer(eo, 'priority'),
				tag: file.string(eo, 'tag')
			})
		: undefined;

	const u = GROUP_USB_DEVICES;
	const d = GROUP_DCONF;
	const extraData = parseExtraData(file);

	return {
		kind,
		mainGroup: main,
		...compact({
			name: mainStr('name'),
			runtime: mainStr('runtime'),
			sdk: mainStr('sdk'),
			command: mainStr('command')
		}),
		requiredFlatpak: (main ? file.stringList(main, 'required-flatpak') : undefined) ?? [],
		// Distinct and non-empty: "tags=a;;a" is accepted by flatpak, and the view keys by tag.
		tags: [...new Set((main ? file.stringList(main, 'tags') : undefined) ?? [])].filter(Boolean),
		context,
		sessionBus: parseBusPolicy(file, GROUP_SESSION_BUS_POLICY),
		systemBus: parseBusPolicy(file, GROUP_SYSTEM_BUS_POLICY),
		a11yBus: parseBusPolicy(file, GROUP_A11Y_BUS_POLICY),
		environment: file
			.keys(GROUP_ENVIRONMENT)
			.map((name) => ({ name, value: file.string(GROUP_ENVIRONMENT, name) ?? '' })),
		policies,
		usb: {
			enumerable: (file.stringList(u, 'enumerable-devices') ?? []).map(parseUsbQuery),
			hidden: (file.stringList(u, 'hidden-devices') ?? []).map(parseUsbQuery)
		},
		...(file.hasGroup(d)
			? { dconf: compact({ paths: file.string(d, 'paths'), migratePath: file.string(d, 'migrate-path') }) }
			: {}),
		extensions,
		...(extensionOf ? { extensionOf } : {}),
		...(extraData ? { extraData } : {}),
		unknownGroups
	};
}

/** Later entries for the same location replace earlier ones (flatpak keeps a hash table). */
function dedupeFilesystems(entries: FilesystemEntry[]): FilesystemEntry[] {
	const byLocation = new Map<string, FilesystemEntry>();
	for (const e of entries) {
		const key = e.kind === 'invalid' ? `invalid:${e.token}` : e.location;
		byLocation.delete(key);
		byLocation.set(key, e);
	}
	return [...byLocation.values()];
}

/** Drops undefined properties (keeps objects comparable and optional props honest). */
function compact<T extends Record<string, unknown>>(obj: T): { [K in keyof T]?: Exclude<T[K], undefined> } {
	return Object.fromEntries(Object.entries(obj).filter(([, v]) => v !== undefined)) as {
		[K in keyof T]?: Exclude<T[K], undefined>;
	};
}

// ---------------------------------------------------------------------------
// Install-time facts derived from the metadata

/**
 * The runtime relevant for installing:
 * - required: an app's runtime, installed as a dependency
 * - extra-data: a runtime/extension with [Extra Data] (and no NoRuntime=true)
 *   runs its apply_extra script inside this runtime during installation
 *   ([ExtensionOf] runtime, else the main group's runtime)
 * - base: informational (the runtime/extension was built against it)
 */
export interface RuntimeDependency {
	pref: string;
	ref: PartialRef | undefined;
	role: 'required' | 'extra-data' | 'base';
}

export function runtimeDependency(meta: FlatpakMetadata, fallback?: string): RuntimeDependency | undefined {
	const make = (pref: string | undefined, role: RuntimeDependency['role']) =>
		pref ? { pref, ref: parsePartialRef(pref), role } : undefined;
	if (meta.kind === 'app') return make(meta.runtime || fallback, 'required');
	if (meta.extraData && !meta.extraData.noRuntime) {
		const pref = meta.extensionOf?.runtime || meta.runtime || fallback;
		if (pref) return make(pref, 'extra-data');
	}
	const pref = meta.runtime || fallback;
	// A runtime's runtime key usually names the runtime itself.
	if (pref && parsePartialRef(pref)?.id === meta.name) return undefined;
	return make(pref, 'base');
}

/** required-flatpak: the minimum version, plus backports to older series. */
export interface RequiredFlatpak {
	minimum?: string;
	backports: string[];
	invalid: string[];
}

/**
 * flatpak_check_required_version: the largest major.minor is the minimum for
 * newer series; the other entries allow backports in their own series.
 */
export function describeRequiredFlatpak(versions: string[]): RequiredFlatpak {
	const valid: { v: string; major: number; minor: number }[] = [];
	const invalid: string[] = [];
	for (const v of versions) {
		const m = /^\s*[+-]?(\d+)\.[+-]?(\d+)\.[+-]?(\d+)/.exec(v);
		if (m) valid.push({ v, major: Number(m[1]), minor: Number(m[2]) });
		else invalid.push(v);
	}
	if (valid.length === 0) return { backports: [], invalid };
	let max = valid[0];
	for (const x of valid) {
		if (x.major > max.major || (x.major === max.major && x.minor > max.minor)) max = x;
	}
	return { minimum: max.v, backports: valid.filter((x) => x !== max).map((x) => x.v), invalid };
}
