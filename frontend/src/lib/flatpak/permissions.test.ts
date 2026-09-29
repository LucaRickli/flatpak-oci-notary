import { ABIWORD, AUDACITY, CHROMIUM } from './fixtures.ts';
import { parseMetadata, parseUsbQuery } from './metadata.ts';
import { describePermissions, describeUsbQuery, type PermissionItem } from './permissions.ts';
import { assert, assertEquals, test } from './test-helpers.ts';

function describe(text: string) {
	const r = parseMetadata(text, { kind: 'app' });
	if (!r.ok) throw new Error('invalid metadata');
	return describePermissions(r.metadata);
}

function items(context: string, extra = ''): PermissionItem[] {
	return describe(`[Application]\nname=org.example.App\n[Context]\n${context}\n${extra}`).sections.flatMap(
		(s) => s.items
	);
}

const severityOf = (context: string, token: string) =>
	items(context).find((i) => i.token === token)?.severity;

test('filesystem severity: broad access is high, read-only drops one tier', () => {
	const fs = (value: string) => severityOf(`filesystems=${value}`, `filesystems=${value}`);
	assertEquals(fs('host'), 'high');
	assertEquals(fs('host:ro'), 'warning');
	assertEquals(fs('host-os:ro'), 'warning');
	assertEquals(fs('home'), 'high');
	assertEquals(fs('home:ro'), 'warning');
	assertEquals(fs('xdg-config'), 'high');
	assertEquals(fs('xdg-config:ro'), 'warning');
	assertEquals(fs('xdg-data/flatpak/overrides'), 'high');
	assertEquals(fs('~/.local'), 'high');
	assertEquals(fs('~/.local/share/flatpak'), 'high');
	assertEquals(fs('~/.local/share/flatpak:ro'), 'notice');
	assertEquals(fs('~/.ssh:ro'), 'high');
	assertEquals(fs('xdg-config/autostart'), 'high');
	assertEquals(fs('~/Games'), 'warning');
	assertEquals(fs('~/Games:ro'), 'notice');
	assertEquals(fs('/tmp'), 'warning');
	assertEquals(fs('xdg-download'), 'notice');
	assertEquals(fs('xdg-config/kdeglobals:ro'), 'notice');
	assertEquals(fs('xdg-run/pipewire-0'), 'notice');
});

test('removed entries are marked and not counted', () => {
	const d = describe('[Application]\nname=a.b.C\n[Context]\nfilesystems=!home;!host:reset\nsockets=!x11\nshared=network');
	const all = d.sections.flatMap((s) => s.items);
	assertEquals(
		all.filter((i) => i.removed).map((i) => i.token),
		['sockets=!x11', 'filesystems=!home', 'filesystems=!host:reset']
	);
	assertEquals(d.counts, { high: 0, warning: 0, notice: 1 });
	assertEquals(d.total, 4);
});

test('x11 is a warning only without Wayland', () => {
	assertEquals(severityOf('sockets=x11', 'sockets=x11'), 'warning');
	assertEquals(severityOf('sockets=x11;wayland', 'sockets=x11'), 'notice');
	assertEquals(severityOf('sockets=fallback-x11;wayland', 'sockets=fallback-x11'), 'notice');
	const conditional = items('sockets=wayland;x11;if:x11:!has-wayland').find((i) => i.label === 'X11 display');
	assertEquals(conditional?.condition, 'only outside a Wayland session');
	assertEquals(conditional?.severity, 'notice');
});

test('x11 listed with fallback-x11 is limited to sessions without Wayland', () => {
	const x11 = (sockets: string) => items(`sockets=${sockets}`).find((i) => i.token === 'sockets=x11');
	const withWayland = x11('x11;fallback-x11;wayland');
	assertEquals(withWayland?.severity, 'notice');
	assertEquals(withWayland?.condition, 'only outside a Wayland session');
	assert(!(withWayland?.detail ?? '').includes('in addition to Wayland'), withWayland?.detail);
	// Without Wayland access, flatpak before 1.19 still always grants X11.
	const withoutWayland = x11('x11;fallback-x11');
	assertEquals(withoutWayland?.severity, 'warning');
	assert(withoutWayland?.condition?.includes('1.19'), withoutWayland?.condition);
	// Unchanged without fallback-x11, or when fallback-x11 is itself removed.
	assertEquals(x11('x11;wayland')?.condition, undefined);
	assertEquals(x11('x11;!fallback-x11')?.severity, 'warning');
	assertEquals(x11('x11;!fallback-x11')?.condition, undefined);
});

test('absolute paths to home directories are rated like home', () => {
	const fs = (value: string) => severityOf(`filesystems=${value}`, `filesystems=${value}`);
	assertEquals(fs('/home'), 'high');
	assertEquals(fs('/home/'), 'high');
	assertEquals(fs('/home:ro'), 'warning');
	assertEquals(fs('/var/home'), 'high');
	assertEquals(fs('/var'), 'high');
	assertEquals(fs('/home/someone'), 'high');
	assertEquals(fs('/root'), 'high');
	assertEquals(fs('/home/someone/.ssh:ro'), 'high');
	assertEquals(fs('/var/home/someone/.config/autostart'), 'high');
	assertEquals(fs('/home/someone/Music'), 'warning');
	assertEquals(fs('/home/someone/Music:ro'), 'notice');
	assertEquals(fs('/homework'), 'warning');
	assertEquals(fs('/opt'), 'warning');
});

test('D-Bus: wildcards cover the prefix name itself', () => {
	const policy = `[Session Bus Policy]
org.freedesktop.Flatpak.*=talk
org.freedesktop.systemd1.*=own
org.freedesktop.secrets.*=talk
org.freedesktop.FlatpakX.*=talk`;
	const list = items('', policy);
	const item = (token: string) => list.find((i) => i.token === token);
	assertEquals(item('org.freedesktop.Flatpak.*=talk')?.severity, 'high');
	assert(item('org.freedesktop.Flatpak.*=talk')?.detail?.includes('flatpak-spawn'));
	assertEquals(item('org.freedesktop.systemd1.*=own')?.severity, 'high');
	assertEquals(item('org.freedesktop.secrets.*=talk')?.severity, 'warning');
	// A prefix without the dot boundary does not match.
	assertEquals(item('org.freedesktop.FlatpakX.*=talk')?.severity, 'notice');
});

test('D-Bus: full bus access and host escapes are high', () => {
	const policy = `[Session Bus Policy]
org.freedesktop.Flatpak=talk
org.freedesktop.*=talk
org.freedesktop.secrets=talk
org.example.App.Helper=own
org.freedesktop.Notifications=talk
org.gnome.Hidden=none
[System Bus Policy]
org.freedesktop.UPower=talk
org.freedesktop.login1=see`;
	const list = items('sockets=session-bus;system-bus', policy);
	const sev = (token: string) => list.find((i) => i.token === token)?.severity;
	assertEquals(sev('sockets=session-bus'), 'high');
	assertEquals(sev('sockets=system-bus'), 'high');
	assertEquals(sev('org.freedesktop.Flatpak=talk'), 'high');
	assertEquals(sev('org.freedesktop.*=talk'), 'high');
	assertEquals(sev('org.freedesktop.secrets=talk'), 'warning');
	assertEquals(sev('org.example.App.Helper=own'), 'notice');
	assertEquals(sev('org.freedesktop.Notifications=talk'), 'notice');
	assertEquals(sev('org.freedesktop.UPower=talk'), 'warning');
	assertEquals(sev('org.freedesktop.login1=see'), 'notice');
	assertEquals(list.find((i) => i.token === 'org.gnome.Hidden=none')?.removed, true);
});

test('devices and features', () => {
	assertEquals(severityOf('devices=all', 'devices=all'), 'high');
	assertEquals(severityOf('devices=dri', 'devices=dri'), 'notice');
	assertEquals(severityOf('devices=input', 'devices=input'), 'warning');
	assertEquals(severityOf('features=devel', 'features=devel'), 'warning');
	assertEquals(severityOf('features=multiarch', 'features=multiarch'), 'notice');
	const unknown = items('devices=quantum').find((i) => i.token === 'devices=quantum');
	assertEquals([unknown?.label, unknown?.mono, unknown?.severity], ['quantum', true, 'notice']);
});

test('sections are grouped and ordered', () => {
	const d = describe(CHROMIUM);
	assertEquals(
		d.sections.map((s) => s.id),
		['network', 'display', 'audio', 'devices', 'filesystem', 'dbus', 'services']
	);
	assertEquals(d.counts.high, 1); // devices=all
	assert(d.counts.warning >= 3); // pcsc, secrets/kwallet, system bus talk
});

test('Fedora samples: AbiWord home access, Audacity host + X11 only', () => {
	const abiword = describe(ABIWORD);
	assertEquals(
		abiword.sections.flatMap((s) => s.items).filter((i) => i.severity === 'high').map((i) => i.token),
		['filesystems=home']
	);
	const audacity = describe(AUDACITY).sections.flatMap((s) => s.items);
	assertEquals(
		audacity.filter((i) => i.severity !== 'notice').map((i) => [i.token, i.severity]),
		[
			['sockets=x11', 'warning'],
			['devices=all', 'high'],
			// Broadest first within a section.
			['filesystems=host', 'high'],
			['filesystems=/tmp', 'warning']
		]
	);
	assertEquals(audacity.find((i) => i.label === 'ALSA_CONFIG_PATH')?.detail?.includes('empty'), true);
});

test('USB, environment and other groups', () => {
	const list = items(
		'unset-environment=LD_PRELOAD;',
		'[Environment]\nFOO=bar\n[USB Devices]\nenumerable-devices=all;cls:03:*;\nhidden-devices=vnd:0fd9+dev:0063;\n[Policy Test]\nkey=a;!b;\n[X-DConf]\nmigrate-path=/org/example/app/'
	);
	const by = (token: string) => list.find((i) => i.token === token);
	assertEquals(by('enumerable-devices=all')?.severity, 'warning');
	assertEquals(by('enumerable-devices=cls:03:*')?.label, 'Class 03: HID (keyboards, mice, controllers)');
	assertEquals(by('hidden-devices=vnd:0fd9+dev:0063')?.removed, true);
	assertEquals(by('unset-environment=LD_PRELOAD')?.removed, true);
	assertEquals([by('FOO=bar')?.detail, by('FOO=bar')?.detailMono], ['bar', true]);
	assertEquals(by('[Policy Test] key=a;!b')?.label, 'Test.key');
	assert(by('[X-DConf]')?.detail?.includes('/org/example/app/'));
	assertEquals(describeUsbQuery(parseUsbQuery('vnd:0fd9+dev:0063')), 'Vendor 0fd9, product 0063');
});

test('no permissions at all', () => {
	const d = describe('[Application]\nname=org.example.Sandboxed\nruntime=a.b.C/x86_64/1');
	assertEquals(d.sections, []);
	assertEquals(d.total, 0);
});
