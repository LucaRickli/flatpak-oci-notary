import { ABIWORD, AUDACITY, CHROMIUM, FEDORA_PLATFORM, OPENH264 } from './fixtures.ts';
import {
	describeRequiredFlatpak,
	parseFilesystem,
	parseFullRef,
	parseMetadata,
	parsePartialRef,
	parsePermissionList,
	parseUsbQuery,
	permissionEffect,
	runtimeDependency,
	safeHttpUrl,
	type FlatpakMetadata
} from './metadata.ts';
import { assert, assertEquals, test } from './test-helpers.ts';

function meta(text: string, kind?: 'app' | 'runtime'): FlatpakMetadata {
	const r = parseMetadata(text, { kind });
	if (!r.ok) throw new Error(`unexpected parse errors: ${JSON.stringify(r.errors)}`);
	return r.metadata;
}

test('Fedora openh264: runtime extension with extra data', () => {
	const m = meta(OPENH264, 'runtime');
	assertEquals(m.kind, 'runtime');
	assertEquals(m.name, 'org.fedoraproject.Platform.Codecs.openh264');
	assertEquals(m.runtime, 'org.fedoraproject.Platform/x86_64/f44');
	assertEquals(m.sdk, 'org.fedoraproject.Sdk/x86_64/f44');
	assertEquals(m.extraData, {
		noRuntime: false,
		sources: [
			{
				suffix: '',
				name: 'openh264.x86_64.rpm',
				uri: 'https://codecs.fedoraproject.org/openh264/44/x86_64/Packages/o/openh264-2.6.0-3.fc44.x86_64.rpm',
				httpUrl: 'https://codecs.fedoraproject.org/openh264/44/x86_64/Packages/o/openh264-2.6.0-3.fc44.x86_64.rpm',
				host: 'codecs.fedoraproject.org',
				size: 445460,
				checksum: 'ddd3123c507f08d9ff1962371ada5dc555742dfa16d4543f87a53c1303c9da6c',
				problems: []
			}
		]
	});
	// The apply_extra script runs inside the runtime, so it is needed at install.
	assertEquals(runtimeDependency(m), {
		pref: 'org.fedoraproject.Platform/x86_64/f44',
		ref: { id: 'org.fedoraproject.Platform', arch: 'x86_64', branch: 'f44' },
		role: 'extra-data'
	});
	assertEquals(m.unknownGroups, []);
});

test('Fedora Platform runtime: extension points and download rules', () => {
	const m = meta(FEDORA_PLATFORM, 'runtime');
	assertEquals(m.kind, 'runtime');
	// The runtime key of a runtime names itself: nothing to install.
	assertEquals(runtimeDependency(m), undefined);
	assertEquals(m.environment.find((e) => e.name === 'LUA_CPATH')?.value, '/app/lib64/lua/5.4/?.so;;');
	const ext = Object.fromEntries(m.extensions.map((e) => [e.name, e]));
	assertEquals(Object.keys(ext).length, 10);

	const gl = ext['org.fedoraproject.Platform.GL'];
	assertEquals(gl.versions, ['f44', '1.4']);
	// download-if decides even though no-autodownload=true.
	assertEquals(gl.download, 'conditional');
	assertEquals(gl.downloadIf, ['active-gl-driver']);
	assertEquals(gl.enableIf, ['active-gl-driver']);
	assertEquals(gl.autopruneUnless, ['active-gl-driver']);
	assertEquals(gl.subdirectories, true);
	assertEquals(gl.addLdPath, 'lib');
	assertEquals(gl.mergeDirs.length, 8);
	assertEquals(gl.removedWithParent, false);

	assertEquals(ext['org.freedesktop.Platform.GL'].versions, ['1.4']);
	assertEquals(ext['org.freedesktop.Platform.GL'].download, 'manual');
	assertEquals(ext['org.fedoraproject.Platform.VAAPI.nvidia'].downloadIf, ['have-kernel-module-nvidia']);
	assertEquals(ext['org.fedoraproject.Platform.Codecs'].download, 'manual');
	assertEquals(ext['org.fedoraproject.Gtk3theme'].subdirectorySuffix, 'gtk-3.0');

	const locale = ext['org.fedoraproject.Platform.Locale'];
	assertEquals(locale.download, 'auto');
	assertEquals(locale.partial, true);
	assertEquals(locale.removedWithParent, true);
	assertEquals(locale.versions, undefined);
	assertEquals(locale.directory, 'share/runtime/locale');
});

test('Fedora AbiWord: app permissions and bus policy', () => {
	const m = meta(ABIWORD, 'app');
	assertEquals(m.kind, 'app');
	assertEquals(m.command, 'abiword');
	assertEquals(runtimeDependency(m)?.role, 'required');
	assertEquals(
		m.context.shared.map((p) => [p.name, p.state]),
		[
			['ipc', 'allowed'],
			['network', 'allowed']
		]
	);
	assertEquals(
		m.context.sockets.map((p) => p.name),
		['fallback-x11', 'wayland']
	);
	assertEquals(
		m.context.filesystems.map((f) => [f.kind, f.location, f.mode]),
		[
			['home', 'home', 'read-write'],
			['xdg', 'xdg-run/gvfsd', 'read-write'],
			['xdg', 'xdg-run/gvfs', 'read-write']
		]
	);
	assertEquals(
		m.sessionBus.map((b) => [b.name, b.policy]),
		[
			['org.freedesktop.Telepathy.Client.AbiCollab', 'own'],
			['org.freedesktop.Telepathy.Client.AbiCollab.*', 'own'],
			['org.gtk.vfs.*', 'talk']
		]
	);
});

test('Fedora Chromium and Audacity parse without problems', () => {
	const c = meta(CHROMIUM, 'app');
	assertEquals(c.systemBus.map((b) => b.name), ['org.freedesktop.Avahi', 'org.freedesktop.UPower', 'org.bluez']);
	assertEquals(c.context.devices.map((d) => [d.name, d.state]), [['all', 'allowed']]);
	assertEquals(c.extensions.every((e) => e.download === 'manual' && e.removedWithParent), true);
	assertEquals(c.context.filesystems.find((f) => f.location === 'xdg-run/pipewire-0')?.mode, 'read-only');
	assertEquals(c.context.filesystems.find((f) => f.kind === 'absolute')?.location, '/run/.heim_org.h5l.kcm-socket');

	const a = meta(AUDACITY, 'app');
	assertEquals(a.environment.find((e) => e.name === 'ALSA_CONFIG_PATH')?.value, '');
	assertEquals(
		a.context.filesystems.map((f) => f.kind),
		['xdg', 'absolute', 'host']
	);
	for (const m of [c, a]) {
		assertEquals(m.context.invalid, []);
		assertEquals(m.context.filesystems.filter((f) => f.problem), []);
		assertEquals([...m.sessionBus, ...m.systemBus].filter((b) => b.problem), []);
	}
});

test('conditional permissions follow flatpak_permissions_from_strv', () => {
	const list = (s: string) => parsePermissionList(s.split(';').filter(Boolean)).permissions;
	const x11 = (s: string) => list(s).find((p) => p.name === 'x11');

	assertEquals(x11('wayland;x11;if:x11:!has-wayland;'), {
		name: 'x11',
		state: 'conditional',
		conditions: [{ name: 'has-wayland', negated: true }]
	});
	assertEquals(x11('!x11;x11;if:x11:!has-wayland;')?.state, 'conditional');
	assertEquals(x11('x11;')?.state, 'allowed');
	assertEquals(x11('x11;!x11')?.state, 'denied');
	assertEquals(x11('!x11')?.state, 'denied');
	// Conditions are ORed; duplicates collapse.
	assertEquals(
		x11('if:x11:has-wayland;if:x11:false;if:x11:has-wayland')?.conditions.map((c) => c.name),
		['false', 'has-wayland']
	);
	// A later unconditional grant wins again.
	assertEquals(x11('if:x11:!has-wayland;x11')?.state, 'allowed');

	const effect = (s: string) => permissionEffect(list(s)[0]);
	assertEquals(effect('if:input:has-input-device'), 'always');
	assertEquals(effect('if:x11:!has-wayland'), 'sometimes');
	assertEquals(effect('if:x11:false'), 'never');
	assertEquals(effect('if:x11:!false'), 'always');
	assertEquals(effect('if:x11:some-future-thing'), 'never');
	assertEquals(effect('if:x11:!some-future-thing'), 'never');

	assertEquals(parsePermissionList(['if:x11', 'x11:foo', 'ok']).invalid, ['if:x11', 'x11:foo']);
});

test('filesystems: modes, negation, normalization and invalid entries', () => {
	const fs = (t: string) => {
		const e = parseFilesystem(t);
		return [e.kind, e.location, e.mode, e.removed, e.problem ?? ''];
	};
	assertEquals(fs('host'), ['host', 'host', 'read-write', false, '']);
	assertEquals(fs('home:ro'), ['home', 'home', 'read-only', false, '']);
	assertEquals(fs('~/Games:create'), ['home-path', '~/Games', 'create', false, '']);
	assertEquals(fs('!xdg-download'), ['xdg', 'xdg-download', 'none', true, '']);
	assertEquals(fs('~'), ['home', 'home', 'read-write', false, '']);
	assertEquals(fs('home/Music'), ['home-path', '~/Music', 'read-write', false, '']);
	assertEquals(fs('/opt//foo/./bar/'), ['absolute', '/opt/foo/bar', 'read-write', false, '']);
	assertEquals(fs('~/'), ['home', 'home', 'read-write', false, '']);
	assertEquals(fs('!host:reset'), ['host-reset', 'host-reset', 'none', true, '']);
	assertEquals(fs('!host-reset')[0], 'host-reset');
	// A backslash escapes ':' in the location.
	assertEquals(fs('~/odd\\:name:ro'), ['home-path', '~/odd:name', 'read-only', false, '']);
	assertEquals(parseFilesystem('xdg-config/kdeglobals:ro').subpath, 'kdeglobals');
	assertEquals(parseFilesystem('xdg-run/pipewire-0').xdgDir, 'xdg-run');

	for (const bad of ['host:reset', '!home:reset', '/', '~/a/../b', '~/..', 'xdg-run', 'nonsense', 'host-reset']) {
		assertEquals(parseFilesystem(bad).kind, 'invalid', bad);
	}
	assert(parseFilesystem('home:bogus').problem?.includes('Unknown suffix'));
	assertEquals(parseFilesystem('home:bogus').mode, 'read-write');
	assertEquals(parseFilesystem('!home:ro').mode, 'none');
	assert(parseFilesystem('!home:ro').problem);

	// Later entries for the same location replace earlier ones.
	const m = meta('[Application]\nname=a.b.C\n[Context]\nfilesystems=home;xdg-download;home:ro;');
	assertEquals(
		m.context.filesystems.map((f) => [f.location, f.mode]),
		[
			['xdg-download', 'read-write'],
			['home', 'read-only']
		]
	);
});

test('extension points: tags, versions and download modes', () => {
	const m = meta(
		[
			'[Application]',
			'name=org.example.App',
			'[Extension org.example.App.Plugin@stable]',
			'directory=plugins',
			'version=2',
			'[Extension org.example.App.Plugin@beta]',
			'directory=plugins-beta',
			'versions=3;4;',
			'no-autodownload=1',
			'[Extension org.example.App.Debug]',
			'directory=lib/debug',
			'[Extension org.example.App.Theme]',
			'directory=themes',
			'download-if=on-xdg-desktop-KDE;active-gtk-theme',
			'locale-subset=true',
			'autodelete=true',
			'[Extension ]',
			'directory=ignored'
		].join('\n')
	);
	assertEquals(
		m.extensions.map((e) => [e.name, e.tag ?? '', e.versions ?? [], e.download]),
		[
			['org.example.App.Plugin', 'stable', ['2'], 'auto'],
			['org.example.App.Plugin', 'beta', ['3', '4'], 'manual'],
			['org.example.App.Debug', '', [], 'debug'],
			['org.example.App.Theme', '', [], 'conditional']
		]
	);
	const theme = m.extensions[3];
	assertEquals(theme.downloadIf, ['on-xdg-desktop-KDE', 'active-gtk-theme']);
	assertEquals([theme.partial, theme.removedWithParent], [true, true]);
	assertEquals(m.extensions[2].removedWithParent, true);
	assertEquals(m.unknownGroups, ['Extension ']);
});

test('extra data: multiple sources, defaults and validation', () => {
	const m = meta(
		[
			'[Application]',
			'name=org.example.Proprietary',
			'runtime=org.example.Platform/x86_64/1',
			'[Extra Data]',
			'NoRuntime=true',
			'uri1=https://example.com/second%20file.tar.gz',
			`checksum1=${'b'.repeat(64)}`,
			'size1=2048',
			'installed-size1=4096',
			`checksum=${'a'.repeat(64)}`,
			'size=1',
			'uri=http://example.com/first.bin',
			'uri2=javascript:alert(1)//x.deb',
			'checksum2=nothex',
			'size2=0',
			'name3=orphan'
		].join('\n')
	);
	const ed = m.extraData;
	assert(ed);
	assertEquals(ed.noRuntime, true);
	assertEquals(
		ed.sources.map((s) => [s.suffix, s.name, s.size, s.installedSize, s.httpUrl, s.problems.length]),
		[
			['', 'first.bin', 1, undefined, 'http://example.com/first.bin', 0],
			['1', 'second file.tar.gz', 2048, 4096, 'https://example.com/second%20file.tar.gz', 0],
			['2', 'x.deb', 0, undefined, undefined, 3]
		]
	);
	// An app's runtime is required regardless of NoRuntime.
	assertEquals(runtimeDependency(m)?.role, 'required');
});

test('safeHttpUrl only accepts absolute http(s) URLs', () => {
	assertEquals(safeHttpUrl('https://example.com/a%20b'), 'https://example.com/a%20b');
	assertEquals(safeHttpUrl('HTTP://Example.com/x'), 'http://example.com/x');
	for (const bad of [
		// Browsers and flatpak (GLib, curl) would resolve different hosts, or the host hides
		// behind credentials.
		'https://codecs.fedoraproject.org\\@evil.example/x.rpm',
		'https://codecs.fedoraproject.org@evil.example/x.rpm',
		'https://user:pass@example.com/x',
		'https://example.com/a b',
		'https://exa\tmple.com/x',
		'https://example.com/x\u0000y',
		'javascript:alert(1)',
		'JaVaScRiPt:alert(1)',
		' https://example.com',
		'data:text/html,<script>alert(1)</script>',
		'//example.com/x',
		'/relative',
		'ftp://example.com/x',
		'https://',
		'file:///etc/passwd',
		'https:example.com'
	]) {
		assertEquals(safeHttpUrl(bad), undefined, bad);
	}
});

test('extra data: ambiguous URIs are flagged and never linked', () => {
	const ed = meta(
		[
			'[Runtime]',
			'name=org.example.Codecs',
			'[Extra Data]',
			// Keyfile escape: the value is https://codecs.fedoraproject.org\@evil.example/x.rpm
			'uri=https://codecs.fedoraproject.org\\\\@evil.example/x.rpm',
			`checksum=${'a'.repeat(64)}`,
			'size=1',
			'uri1=https://codecs.fedoraproject.org/ok.rpm',
			`checksum1=${'a'.repeat(64)}`,
			'size1=1'
		].join('\n'),
		'runtime'
	).extraData;
	assert(ed);
	const [bad, good] = ed.sources;
	assertEquals(bad.uri, 'https://codecs.fedoraproject.org\\@evil.example/x.rpm');
	assertEquals(bad.httpUrl, undefined);
	assertEquals(bad.host, undefined);
	assert(bad.problems.some((p) => p.includes('different hosts')), JSON.stringify(bad.problems));
	assertEquals(good.httpUrl, 'https://codecs.fedoraproject.org/ok.rpm');
	assertEquals(good.host, 'codecs.fedoraproject.org');
	assertEquals(good.problems, []);
});

test('tags are distinct and non-empty', () => {
	assertEquals(meta('[Application]\nname=org.example.App\ntags=x;x').tags, ['x']);
	assertEquals(meta('[Application]\nname=org.example.App\ntags=beta;;devel;;beta').tags, ['beta', 'devel']);
});

test('ExtensionOf, runtime roles and refs', () => {
	const ext = meta(
		'[Runtime]\nname=org.example.App.Plugin\nruntime=org.example.Platform/x86_64/1\n[ExtensionOf]\nref=app/org.example.App/x86_64/stable\nruntime=org.example.Platform/x86_64/2\npriority=5\ntag=beta',
		'runtime'
	);
	assertEquals(ext.extensionOf, {
		ref: 'app/org.example.App/x86_64/stable',
		runtime: 'org.example.Platform/x86_64/2',
		priority: 5,
		tag: 'beta'
	});
	// Without extra data the runtime is informational.
	assertEquals(runtimeDependency(ext)?.role, 'base');
	assertEquals(runtimeDependency(ext)?.pref, 'org.example.Platform/x86_64/1');

	// With extra data, [ExtensionOf] runtime is the one needed at install.
	const withData = meta(
		`[Runtime]\nname=x.y.Z\nruntime=a.b.C/x86_64/1\n[ExtensionOf]\nruntime=a.b.C/x86_64/2\n[Extra Data]\nuri=https://e.com/f\nchecksum=${'c'.repeat(64)}\nsize=1`,
		'runtime'
	);
	assertEquals(runtimeDependency(withData)?.pref, 'a.b.C/x86_64/2');
	assertEquals(runtimeDependency(withData)?.role, 'extra-data');
	// Falls back to the image's runtime when the metadata has none.
	assertEquals(runtimeDependency(meta('[Application]\nname=a.b.C'), 'x.y.Z/aarch64/3')?.ref, {
		id: 'x.y.Z',
		arch: 'aarch64',
		branch: '3'
	});

	assertEquals(parsePartialRef('org.x.Y/x86_64/stable'), { id: 'org.x.Y', arch: 'x86_64', branch: 'stable' });
	assertEquals(parsePartialRef('org.x.Y'), { id: 'org.x.Y', arch: '', branch: '' });
	assertEquals(parsePartialRef('a/b/c/d'), undefined);
	assertEquals(parseFullRef('runtime/org.x.Y/x86_64/1'), { kind: 'runtime', id: 'org.x.Y', arch: 'x86_64', branch: '1' });
	assertEquals(parseFullRef('org.x.Y/x86_64/1'), undefined);
});

test('main group follows the image kind when both exist', () => {
	const text = '[Application]\nname=app.id.A\n[Runtime]\nname=rt.id.R';
	assertEquals(meta(text).kind, 'app');
	assertEquals(meta(text, 'runtime').name, 'rt.id.R');
	assertEquals(meta('[Context]\nshared=network').kind, undefined);
});

test('USB queries', () => {
	assertEquals(parseUsbQuery('vnd:0FD9+dev:0063').rules, [
		{ type: 'vendor', id: '0fd9' },
		{ type: 'product', id: '0063' }
	]);
	assertEquals(parseUsbQuery('cls:03:*').rules, [{ type: 'class', class: '03', subclass: undefined }]);
	assertEquals(parseUsbQuery('all').problem, undefined);
	for (const bad of ['dev:0063', 'vnd:xyz', 'all+vnd:0fd9', '0fd9:*', 'vnd:0fd9+vnd:0001', '']) {
		assert(parseUsbQuery(bad).problem, bad);
	}
	const m = meta('[Application]\nname=a.b.C\n[USB Devices]\nenumerable-devices=vnd:0fd9;cls:0b:*;\nhidden-devices=vnd:0fd9+dev:0063;');
	assertEquals(m.usb.enumerable.length, 2);
	assertEquals(m.usb.hidden[0].raw, 'vnd:0fd9+dev:0063');
});

test('bus names and policies are validated', () => {
	const m = meta('[Application]\nname=a.b.C\n[Session Bus Policy]\norg.ok.Name=talk\nnodots=talk\norg.ok.Other=bogus\n[Accessibility Bus Policy]\norg.a11y.Bus=own');
	assertEquals(
		m.sessionBus.map((b) => [b.name, b.policy ?? '', !!b.problem]),
		[
			['org.ok.Name', 'talk', false],
			['nodots', 'talk', true],
			['org.ok.Other', '', true]
		]
	);
	assertEquals(m.a11yBus[0].policy, 'own');
});

test('required-flatpak', () => {
	assertEquals(describeRequiredFlatpak(['1.6.2', '1.4.2']), { minimum: '1.6.2', backports: ['1.4.2'], invalid: [] });
	assertEquals(describeRequiredFlatpak(['1.4.2', '1.6.2']).minimum, '1.6.2');
	assertEquals(describeRequiredFlatpak(['1.0']), { backports: [], invalid: ['1.0'] });
	assertEquals(describeRequiredFlatpak([]), { backports: [], invalid: [] });
});

test('invalid key files are reported, not interpreted', () => {
	const r = parseMetadata('name=x\n[Application]\nname=y');
	assertEquals(r.ok, false);
	if (!r.ok) assertEquals(r.errors[0].line, 1);
	const empty = parseMetadata('');
	assert(empty.ok);
	assertEquals(empty.metadata.kind, undefined);
});
