// DOM-independent command generator for the static GitHub Pages tool.
export const DEFAULTS = Object.freeze({
  count: 10, speed: 1, delay: 1000, threads: 200, port: 443, sample: 0,
  dlTimeout: 10, maxRun: 0, output: 'result.csv', uploadLimit: 0,
});

export const RELEASES = Object.freeze({
  windows: { amd64: '.\\yx_windows_amd64.exe', arm64: '.\\yx_windows_arm64.exe' },
  linux: { amd64: './yx_linux_amd64', arm64: './yx_linux_arm64', '386': './yx_linux_386' },
  darwin: { amd64: './yx_darwin_amd64', arm64: './yx_darwin_arm64' },
  freebsd: { amd64: './yx_freebsd_amd64' },
});

const isSet = (v) => v !== undefined && v !== null && String(v) !== '';
const bool = (v) => v === true || v === 'true' || v === 'on';

export function executableFor(platform, arch, override = '') {
  if (override.trim()) return override.trim();
  return RELEASES[platform]?.[arch] ?? '';
}

export function validate(values = {}) {
  const errors = {};
  const number = (key, min, label) => {
    if (!isSet(values[key])) return;
    const n = Number(values[key]);
    if (!Number.isFinite(n) || n < min) errors[key] = `${label}必须是不小于 ${min} 的数字`;
  };
  number('count', 1, '测速数量'); number('speed', 0, '速度下限'); number('delay', 1, '延迟上限');
  number('threads', 1, '线程数'); number('port', 1, '端口'); number('sample', 0, '候选数量');
  number('dlTimeout', 1, '下载超时'); number('maxRun', 0, '最大运行时间'); number('uploadLimit', 0, '上报数量');
  if (!['posix', 'powershell'].includes(values.shell ?? 'posix')) errors.shell = '请选择命令行类型';
  if (values.upload && !['api', 'worker', 'github', 'telegram'].includes(values.upload)) errors.upload = '上报方式无效';
  if (values.notify && values.notify !== 'feishu') errors.notify = '通知方式仅支持 feishu';
  if (values.notify === 'feishu') {
    for (const [key, label] of [['feishuAppID', '飞书 App ID'], ['feishuAppSecret', '飞书 App Secret'], ['feishuReceiveID', '飞书接收 ID']]) {
      if (!String(values[key] ?? '').trim()) errors[key] = `${label}不能为空`;
    }
    if (values.feishuReceiveIDType && !['chat_id', 'open_id', 'union_id', 'user_id', 'email'].includes(values.feishuReceiveIDType)) errors.feishuReceiveIDType = '接收 ID 类型无效';
  }
  if (values.platform && !RELEASES[values.platform]) errors.platform = '平台无效';
  if (values.platform && values.arch && !RELEASES[values.platform]?.[values.arch]) errors.arch = '该平台不支持此架构';
  return errors;
}

function quotePosix(value) {
  const text = String(value);
  if (/^[A-Za-z0-9_@%+=:,./\\-]+$/.test(text)) return text;
  return `'${text.replaceAll("'", "'\\''")}'`;
}
function quotePowerShell(value) {
  const text = String(value);
  if (/^[A-Za-z0-9_@%+=:,./\\-]+$/.test(text)) return text;
  return `'${text.replaceAll("'", "''")}'`;
}
function token(flag, value, shell) { return [flag, shell === 'powershell' ? quotePowerShell(value) : quotePosix(value)]; }

export function buildArgv(values = {}) {
  const argv = [];
  const add = (flag, value) => { if (isSet(value)) argv.push(...token(flag, value, values.shell ?? 'posix')); };
  const addNumber = (flag, key, fallback) => { if (isSet(values[key]) && Number(values[key]) !== fallback) add(flag, values[key]); };
  const addBool = (flag, key) => { if (bool(values[key])) argv.push(flag); };
  add('-colo', values.colo); addBool('-ipv6', 'ipv6'); addNumber('-n', 'count', DEFAULTS.count);
  addNumber('-sl', 'speed', DEFAULTS.speed); addNumber('-tl', 'delay', DEFAULTS.delay); addNumber('-t', 'threads', DEFAULTS.threads);
  addNumber('-port', 'port', DEFAULTS.port); add('-url', values.url); add('-f', values.ipFile); addNumber('-c', 'sample', DEFAULTS.sample);
  addBool('-all', 'all'); addBool('-http', 'httping'); addBool('-nodl', 'noDL'); addNumber('-dt', 'dlTimeout', DEFAULTS.dlTimeout);
  addNumber('-mt', 'maxRun', DEFAULTS.maxRun); add('-o', values.output === DEFAULTS.output ? '' : values.output);
  if (values.upload) {
    add('-upload', values.upload);
    const uploadFields = { api: [['-domain', 'domain'], ['-uuid', 'uuid']], worker: [['-worker-url', 'workerURL'], ['-worker-token', 'workerToken']], github: [['-repo', 'repo'], ['-token', 'token'], ['-path', 'path']], telegram: [['-bot-token', 'botToken'], ['-chat-id', 'chatID']] };
    for (const [flag, key] of uploadFields[values.upload] ?? []) add(flag, values[key]);
    addNumber('-limit', 'uploadLimit', DEFAULTS.uploadLimit); addBool('-clear', 'clear');
  }
  if (values.notify === 'feishu') {
    add('-notify', 'feishu'); add('-feishu-app-id', values.feishuAppID); add('-feishu-app-secret', values.feishuAppSecret);
    add('-feishu-receive-id', values.feishuReceiveID); add('-feishu-receive-id-type', values.feishuReceiveIDType === 'chat_id' ? '' : values.feishuReceiveIDType);
  }
  return argv;
}

export function generateCommand(values = {}) {
  const errors = validate(values);
  const executable = executableFor(values.platform ?? 'linux', values.arch ?? 'amd64', values.executable ?? '');
  if (!executable) errors.executable = '请选择平台和架构，或填写可执行文件路径';
  const argv = buildArgv(values);
  const command = [values.shell === 'powershell' ? quotePowerShell(executable) : quotePosix(executable), ...argv].join(' ');
  const secretKeys = new Set(['token', 'workerToken', 'botToken', 'feishuAppSecret']);
  const displayArgv = buildArgv(values).map((part, i) => {
    if (!secretKeys.has(values._lastKeyForArg?.[i])) return part;
    return '••••••';
  });
  // Mask by replacing exact secret values, including values embedded in quoted argv.
  let displayCommand = command;
  for (const key of secretKeys) if (isSet(values[key])) {
    const raw = String(values[key]); displayCommand = displayCommand.replaceAll(raw, '••••••');
  }
  return { command, displayCommand, argv, errors, valid: Object.keys(errors).length === 0, executable };
}

export function maskCommand(command, values = {}) {
  let output = command;
  for (const key of ['token', 'workerToken', 'botToken', 'feishuAppSecret']) if (isSet(values[key])) output = output.replaceAll(String(values[key]), '••••••');
  return output;
}
