# 为 spd_worker.js 增加 yx-tools 直传接口

以下改动让 yx-tools 把 GitHub 格式的前 N 条结果直接写入 Worker KV。

## 1. 注册受保护路由

在 `PROTECTED_API_PATHS` 中加入：

```js
'/upload-fast-ips',
```

在 `switch (path)` 中加入：

```js
case '/upload-fast-ips':
  if (request.method !== 'POST') return jsonResponse({ error: 'Method not allowed' }, 405);
  return await handleUploadFastIPs(env, request);
```

如果希望兼容 GitHub 文件名，可让 `/niceip.txt` 与 `/fast-ips.txt` 共用读取函数：

```js
case '/niceip.txt':
case '/fast-ips.txt':
  return await handleGetFastIPsText(env, request);
```

## 2. 增加上传处理函数

```js
async function handleUploadFastIPs(env, request) {
  let body;
  try {
    body = await request.json();
  } catch {
    return jsonResponse({ success: false, error: 'Body must be JSON' }, 400);
  }

  const parsed = parseYXToolsNiceIPContent(body.content);
  if (parsed.error) {
    return jsonResponse({ success: false, error: parsed.error }, 400);
  }
  if (body.count != null && Number(body.count) !== parsed.fastIPs.length) {
    return jsonResponse({ success: false, error: 'count does not match content' }, 400);
  }

  const stored = {
    fastIPs: parsed.fastIPs,
    categorizedIPs: categorizeByISP(parsed.fastIPs),
    lastTested: new Date().toISOString(),
    count: parsed.fastIPs.length,
    testedCount: parsed.fastIPs.length,
    totalIPs: parsed.fastIPs.length,
    source: 'yx-tools'
  };

  await Promise.all([
    env.IP_STORAGE.put('cloudflare_fast_ips', JSON.stringify(stored)),
    env.IP_STORAGE.put('niceip_content', parsed.content)
  ]);

  return jsonResponse({
    success: true,
    count: parsed.fastIPs.length,
    textURL: '/fast-ips.txt'
  });
}

function parseYXToolsNiceIPContent(value) {
  if (typeof value !== 'string') return { error: 'content must be a string' };
  const content = value.replace(/\r\n/g, '\n').trim();
  if (!content || content.length > 16384) return { error: 'content is empty or too large' };

  const lines = content.split('\n');
  if (lines.length > FAST_IP_COUNT) return { error: `at most ${FAST_IP_COUNT} IPs are allowed` };

  const fastIPs = [];
  const seen = new Set();
  const pattern = /^([0-9A-Fa-f:.]+)#([1-9]\d*) \| ([A-Z]{2}) \| ([^|\r\n]{1,64}) \| (\d+\.\d{2})MB\/s$/;

  for (let i = 0; i < lines.length; i++) {
    const match = lines[i].match(pattern);
    if (!match) return { error: `invalid format on line ${i + 1}` };

    const [, ip, sequenceText, countryCode, provider, speedText] = match;
    const validIP = ip.includes(':')
      ? ip.length <= 45 && /^[0-9A-Fa-f:]+$/.test(ip)
      : isValidIPv4(ip);
    if (!validIP) return { error: `invalid IP on line ${i + 1}` };
    if (Number(sequenceText) !== i + 1) return { error: `invalid sequence on line ${i + 1}` };
    if (seen.has(ip)) return { error: `duplicate IP on line ${i + 1}` };
    seen.add(ip);

    fastIPs.push({
      ip,
      latency: 0,
      bandwidth: Number(speedText),
      isp: ISP_TYPES.OTHER,
      countryCode,
      provider: provider.trim(),
      sequence: i + 1,
      source: 'yx-tools'
    });
  }

  return { content, fastIPs };
}
```

## 3. 让文本接口优先返回原文

在 `handleGetFastIPsText` 开头加入：

```js
const uploadedContent = await env.IP_STORAGE.get('niceip_content');
if (uploadedContent) {
  return new Response(uploadedContent, {
    headers: {
      'Content-Type': 'text/plain; charset=utf-8',
      'Content-Disposition': 'inline; filename="niceip.txt"',
      'Access-Control-Allow-Origin': '*'
    }
  });
}
```

在 `handleClearUploadedIPs` 中增加：

```js
await env.IP_STORAGE.delete('niceip_content');
```

如果继续使用 Worker 自带测速，在 `finalizeSpeedtestRun` 写入新的
`cloudflare_fast_ips` 前也删除 `niceip_content`，避免文本接口返回旧的直传结果。
