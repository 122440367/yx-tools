import { DEFAULTS, RELEASES, generateCommand, maskCommand } from './generator.mjs';

const state = { platform: 'linux', arch: 'amd64', shell: 'posix', count: DEFAULTS.count, speed: DEFAULTS.speed, delay: DEFAULTS.delay, threads: DEFAULTS.threads, port: DEFAULTS.port, sample: DEFAULTS.sample, dlTimeout: DEFAULTS.dlTimeout, maxRun: DEFAULTS.maxRun, output: DEFAULTS.output, uploadLimit: DEFAULTS.uploadLimit };

export function init(root = document) {
  const form = root.querySelector('#command-form');
  if (!form) return;
  const preview = root.querySelector('#command-preview');
  const feedback = root.querySelector('#feedback');
  const copy = root.querySelector('#copy-command');
  const reveal = root.querySelector('#reveal-secret');
  let generated = generateCommand(state);
  const update = () => {
    const values = Object.fromEntries(new FormData(form));
    for (const el of form.querySelectorAll('input[type=checkbox]')) values[el.name] = el.checked;
    Object.assign(state, values);
    generated = generateCommand(state);
    preview.textContent = reveal.checked ? generated.command : generated.displayCommand;
    copy.disabled = !generated.valid;
    preview.classList.toggle('invalid', !generated.valid);
    root.querySelectorAll('[data-error-for]').forEach((el) => { el.textContent = generated.errors[el.dataset.errorFor] ?? ''; });
    root.querySelectorAll('[data-conditional]').forEach((el) => { el.hidden = !state[el.dataset.conditional]; });
  };
  form.addEventListener('input', update); form.addEventListener('change', update);
  reveal.addEventListener('change', update);
  copy.addEventListener('click', async () => {
    if (!generated.valid) return;
    try { await navigator.clipboard.writeText(generated.command); feedback.textContent = '命令已复制（真实值仅用于复制，不会保存）'; }
    catch { feedback.textContent = '浏览器未授权自动复制，请先显示敏感值后手动选择预览文本'; }
  });
  update();
}

if (typeof document !== 'undefined') document.addEventListener('DOMContentLoaded', () => init());

export { state, RELEASES, maskCommand };
