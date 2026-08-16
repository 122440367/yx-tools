import test from 'node:test';
import assert from 'node:assert/strict';
import { RELEASES, executableFor, generateCommand, validate } from './generator.mjs';

test('compact defaults and platform artifact names', () => {
  const result = generateCommand({ platform: 'linux', arch: 'arm64' });
  assert.equal(result.command, './yx_linux_arm64');
  assert.equal(executableFor('windows', 'amd64'), '.\\yx_windows_amd64.exe');
  assert.equal(executableFor('darwin', 'amd64'), './yx_darwin_amd64');
  assert.ok(RELEASES.freebsd.amd64);
});

test('all upload modes keep conditional flags', () => {
  for (const [mode, needle] of [['api', '-domain'], ['worker', '-worker-url'], ['github', '-repo'], ['telegram', '-bot-token']]) {
    const r = generateCommand({ platform: 'linux', arch: 'amd64', upload: mode, domain: 'a b', workerURL: 'https://w', repo: 'o/r', botToken: 'b"x', feishuAppSecret: '' });
    assert.ok(r.command.includes('-upload'), mode);
    assert.ok(r.command.includes(needle), mode);
  }
});

test('feishu is independent of upload and secret is masked only in preview', () => {
  const r = generateCommand({ platform: 'linux', arch: 'amd64', notify: 'feishu', feishuAppID: 'cli_a', feishuAppSecret: 's e c', feishuReceiveID: 'ou_x', feishuReceiveIDType: 'open_id' });
  assert.ok(r.valid);
  assert.ok(r.command.includes('-notify') && r.command.includes('-feishu-receive-id-type'));
  assert.ok(r.command.includes('s e c'));
  assert.ok(!r.displayCommand.includes('s e c') && r.displayCommand.includes('••••••'));
});

test('quoting remains one line for POSIX and PowerShell', () => {
  for (const shell of ['posix', 'powershell']) {
    const r = generateCommand({ shell, platform: 'linux', arch: 'amd64', executable: 'my tools/yx\'bin', url: 'https://x/a b?q="z"', ipFile: 'a file.txt' });
    assert.equal(r.command.includes('\n'), false);
    assert.equal(r.valid, true);
  }
});

test('validation catches boundaries and missing notification fields', () => {
  const e = validate({ shell: 'posix', count: 0, delay: -1, notify: 'feishu', feishuAppID: 'a' });
  assert.ok(e.count && e.delay && e.feishuAppSecret && e.feishuReceiveID);
  assert.equal(Object.keys(validate({ shell: 'posix', notify: '' })).length, 0);
});

test('manual executable override and defaults are respected', () => {
  const r = generateCommand({ platform: 'freebsd', arch: 'amd64', executable: '/opt/yx test', count: 20, speed: 0 });
  assert.ok(r.command.startsWith("'/opt/yx test'"));
  assert.ok(r.command.includes('-n 20'));
  assert.ok(r.command.includes('-sl 0'));
});
