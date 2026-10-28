// 抖音 web 接口签名器
//
// 原理：抖音的 x-secsdk-web-signature 由 secsdk 运行时（runtime_bundler + webmssdk）在浏览器里
// 为每个请求实时计算。这里用 jsdom 提供 DOM 环境，把同一套运行时加载进来，直接调用它暴露的
// DI 服务 webSignUrl 生成签名，从而完全不需要启动浏览器。
//
// 用法：
//   node signer.js <aweme_id>            # 取作品详情，打印播放地址
//   node signer.js --serve [port]        # 起本地签名服务（默认 8099）
//
// HTTP 接口：
//   GET  /health                         -> { ok, uptime, bootMs }
//   GET  /detail?aweme_id=xxx            -> 抖音原始 JSON
//   POST /sign   {"url": "..."}          -> { url, headers }

const fs = require('fs');
const path = require('path');
const http = require('http');
const crypto = require('crypto');
const { JSDOM, VirtualConsole } = require('jsdom');

const UA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36';
const AID = 6383;
const PAGE_ID = 6241;

// 页面里真实加载的四个安全脚本，顺序不能变：
// runtime_bundler 提供 DI 容器（use / registToGlobal），webmssdk 注册 webSignUrl 服务，
// sdk-glue + bdms 负责初始化与 msToken 领取。
const BUNDLES = [
  ['runtime_bundler_34.js', 'https://lf-security.bytegoofy.com/obj/security-secsdk/runtime_bundler_34.js'],
  ['webmssdk.es5.js', 'https://lf-c-flwb.bytetos.com/obj/rc-client-security/c-webmssdk/1.0.0.20/webmssdk.es5.js'],
  ['sdk-glue.js', 'https://lf-c-flwb.bytetos.com/obj/rc-client-security/web/glue/1.0.0.64-fix.01/sdk-glue.js'],
  ['bdms.js', 'https://lf-c-flwb.bytetos.com/obj/rc-client-security/web/stable/1.0.1.19-fix.01/bdms.js'],
];

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

class DouyinSigner {
  constructor(opts = {}) {
    this.dir = opts.dir || __dirname;
    this.vendor = opts.vendor || path.join(this.dir, 'vendor');
    this.ttwid = opts.ttwid || '';
    this.state = null;
    this.ready = null;
    this.bootMs = 0;
  }

  async init() {
    if (!this.ready) this.ready = this._init();
    return this.ready;
  }

  async _init() {
    const t0 = Date.now();
    const virtualConsole = new VirtualConsole();
    virtualConsole.on('jsdomError', () => {}); // canvas 指纹不可用属于预期，忽略

    const dom = new JSDOM('<!DOCTYPE html><html><head></head><body></body></html>', {
      url: 'https://www.douyin.com/',
      runScripts: 'dangerously',
      pretendToBeVisual: true,
      userAgent: UA,
      virtualConsole,
    });
    const win = dom.window;
    win.requestIdleCallback = (cb) => win.setTimeout(() => cb({ didTimeout: false, timeRemaining: () => 50 }), 1);
    // jsdom 没有实现这些 Fetch 相关构造器，运行时初始化会用到
    for (const k of ['Request', 'Response', 'Headers', 'AbortController', 'AbortSignal', 'FormData', 'Blob']) {
      if (globalThis[k] && !win[k]) win[k] = globalThis[k];
    }

    // runtime_bundler 启动时靠这个 script 节点读取 project-id 等属性，缺了会抛空指针
    const scriptEl = win.document.createElement('script');
    scriptEl.setAttribute('src', BUNDLES[0][1]);
    win.document.head.appendChild(scriptEl);

    for (const [file, src] of BUNDLES) {
      Object.defineProperty(win.document, 'currentScript', {
        get: () => ({ tagName: 'SCRIPT', src, getAttribute: () => src, dataset: {}, nonce: '' }),
        configurable: true,
      });
      win.eval(fs.readFileSync(path.join(this.vendor, file), 'utf8'));
    }

    // bdms 初始化后会向 mssdk 领 msToken，令牌通过响应头 x-ms-token 下发
    let msToken = '';
    const NativeXHR = win.XMLHttpRequest;
    const nativeOpen = NativeXHR.prototype.open;
    NativeXHR.prototype.open = function (method, url, ...rest) {
      if (/mssdk.*\/token/.test(String(url))) {
        this.addEventListener('loadend', () => {
          try {
            const t = this.getResponseHeader('x-ms-token');
            if (t) msToken = t;
          } catch (e) { /* 忽略 */ }
        });
      }
      return nativeOpen.call(this, method, url, ...rest);
    };

    // ttwid 注册与 SDK 初始化互不依赖，并行跑，省掉一次串行等待
    const ttwidPromise = this.ttwid ? Promise.resolve(this.ttwid) : this._registerTtwid();

    win._SdkGlueInit({
      self: { aid: AID, pageId: PAGE_ID },
      // ddrt/ic 是官方的启动延迟（页面上是 8.5 秒），我们不需要等待，压到 0.3 秒
      bdms: { aid: AID, pageId: PAGE_ID, paths: ['^/aweme/v1/', '^/aweme/v2/'], boe: false, ddrt: 0.3, ic: 0.3 },
    });

    const deadline = Date.now() + 5000;
    while (!msToken && Date.now() < deadline) await sleep(50);

    this.ttwid = await ttwidPromise;

    this.state = {
      win,
      msToken,
      sign: win.use('webSignUrl'),
      uifid: crypto.randomBytes(120).toString('hex'),
    };
    this.bootMs = Date.now() - t0;
    return this.state;
  }

  async _registerTtwid() {
    try {
      const res = await fetch('https://ttwid.bytedance.com/ttwid/union/register/', {
        method: 'POST',
        headers: { 'content-type': 'application/json', 'user-agent': UA },
        signal: AbortSignal.timeout(8000),
        body: JSON.stringify({
          region: 'cn', aid: 1768, needFid: false, service: 'www.douyin.com',
          migrate_info: { ticket: '', source: 'node' }, cbUrlProtocol: 'https', union: true,
        }),
      });
      const cookies = res.headers.getSetCookie ? res.headers.getSetCookie() : [];
      for (const c of cookies) if (c.startsWith('ttwid=')) return c.split(';')[0];
    } catch (e) { /* 拿不到就用空 cookie，部分场景仍可用 */ }
    return '';
  }

  buildQuery(params) {
    const q = new URLSearchParams();
    q.set('device_platform', 'webapp');
    q.set('aid', String(AID));
    q.set('channel', 'channel_pc_web');
    for (const [k, v] of Object.entries(params)) q.set(k, String(v));
    q.set('pc_client_type', '1');
    q.set('version_code', '190500');
    q.set('version_name', '19.5.0');
    q.set('cookie_enabled', 'true');
    q.set('platform', 'PC');
    q.set('browser_language', 'zh-CN');
    q.set('browser_platform', 'MacIntel');
    q.set('browser_name', 'Chrome');
    q.set('browser_version', '124.0.0.0');
    q.set('browser_online', 'true');
    q.set('engine_name', 'Blink');
    q.set('engine_version', '124.0.0.0');
    q.set('os_name', 'Mac OS');
    q.set('os_version', '10.15.7');
    q.set('device_memory', '8');
    q.set('screen_width', '1920');
    q.set('screen_height', '1080');
    // 不要 HEVC/DASH，拿兼容性更好的 H.264 MP4
    q.set('support_h265', '0');
    q.set('support_dash', '0');
    // 签名强依赖这三个参数：URL 里没有 uifid / timestamp / msToken 时运行时直接返回原样
    q.set('uifid', this.state.uifid);
    q.set('timestamp', String(Math.floor(Date.now() / 1000)));
    if (this.state.msToken) q.set('msToken', this.state.msToken);
    return q.toString();
  }

  // 给任意 /aweme/v1 或 /aweme/v2 URL 加签名
  signUrl(rawUrl) {
    const out = this.state.sign(rawUrl) || {};
    return { url: out.url || rawUrl, headers: out.headers || {} };
  }

  async request(rawUrl) {
    for (let attempt = 0; attempt < 2; attempt++) {
      const signed = this.signUrl(rawUrl);
      const res = await fetch(signed.url, {
        headers: {
          'user-agent': UA,
          accept: 'application/json, text/plain, */*',
          'accept-language': 'zh-CN,zh;q=0.9',
          referer: 'https://www.douyin.com/',
          origin: 'https://www.douyin.com',
          cookie: this.ttwid,
          ...signed.headers,
        },
      });
      const text = await res.text();
      // 签名失效时服务端返回 200 + 空 body，重新初始化一次再试
      if (res.status === 200 && text.length > 0) return { status: res.status, text };
      if (attempt === 0) {
        // 丢弃旧运行时，下一轮会用新 state 重新签名同一个 URL
        this.ready = null;
        await this.init();
      } else {
        return { status: res.status, text };
      }
    }
  }

  async detail(awemeId) {
    const q = this.buildQuery({ aweme_id: awemeId, request_source: 600, origin_type: 'video_page' });
    return this.request(`https://www.douyin.com/aweme/v1/web/aweme/detail/?${q}`);
  }
}

module.exports = { DouyinSigner };

// ---------------------------------------------------------------- CLI / 服务

function pickPlayUrls(detail) {
  const video = (detail && detail.video) || {};
  const list = [];
  for (const item of video.bit_rate || []) {
    const urls = (item.play_addr && item.play_addr.url_list) || [];
    if (urls.length) list.push({ gear: item.gear_name, quality: item.quality_type, size: item.play_addr.data_size, url: urls[0] });
  }
  if (!list.length) {
    const urls = (video.play_addr && video.play_addr.url_list) || [];
    if (urls.length) list.push({ gear: 'default', url: urls[0] });
  }
  return list;
}

// pipe 有缓冲且同步写能保证时序，排查启动卡顿比 console.log 可靠
const log = (msg) => {
  try {
    fs.writeSync(1, `${msg}\n`);
  } catch (e) {
    console.log(msg);
  }
};

async function serve(port, idleMs = 0) {
  const t0 = Date.now();
  const signer = new DouyinSigner();
  await signer.init();
  log(`[signer] init ${Date.now() - t0}ms | boot ${signer.bootMs}ms | listening on 127.0.0.1:${port}`);

  let inflight = 0;
  let lastActive = Date.now();

  const server = http.createServer(async (req, res) => {
    lastActive = Date.now();
    inflight += 1;
    res.on('close', () => {
      inflight -= 1;
      lastActive = Date.now();
    });

    const send = (code, obj) => {
      const body = JSON.stringify(obj);
      res.writeHead(code, { 'content-type': 'application/json; charset=utf-8', 'content-length': Buffer.byteLength(body) });
      res.end(body);
    };
    try {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/health') return send(200, { ok: true, bootMs: signer.bootMs, uptime: process.uptime() });
      if (u.pathname === '/detail') {
        const id = u.searchParams.get('aweme_id');
        if (!id) return send(400, { error: 'aweme_id required' });
        const r = await signer.detail(id);
        res.writeHead(r.status, { 'content-type': 'application/json; charset=utf-8' });
        return res.end(r.text);
      }
      if (u.pathname === '/sign' && req.method === 'POST') {
        let body = '';
        for await (const chunk of req) body += chunk;
        const { url } = JSON.parse(body || '{}');
        if (!url) return send(400, { error: 'url required' });
        return send(200, signer.signUrl(url));
      }
      send(404, { error: 'not found' });
    } catch (e) {
      send(500, { error: e.message });
    }
  });

  // 空闲自退：Go 侧是按需拉起，没人用的时候不该一直占 170MB 内存。
  // 有请求在处理时不计入空闲，避免把正在跑的详情请求掐断。
  if (idleMs > 0) {
    setInterval(() => {
      if (inflight > 0) {
        lastActive = Date.now();
        return;
      }
      if (Date.now() - lastActive < idleMs) return;
      console.log(`[signer] idle for ${Math.round(idleMs / 1000)}s, exiting`);
      server.close(() => process.exit(0));
      setTimeout(() => process.exit(0), 1000).unref();
    }, 5000).unref();
  }

  server.listen(port, '127.0.0.1', () => log(`[signer] accept ready after ${Date.now() - t0}ms`));
}

if (require.main === module) {
  const args = process.argv.slice(2);
  if (args[0] === '--serve') {
    const portArg = args.find((a) => /^\d+$/.test(a));
    const idleIdx = args.indexOf('--idle-timeout');
    const idleSec = idleIdx >= 0 ? Number(args[idleIdx + 1]) || 0 : 0;
    serve(Number(portArg || 8099), idleSec > 0 ? idleSec * 1000 : 0);
  } else {
    (async () => {
      const signer = new DouyinSigner();
      const t0 = Date.now();
      await signer.init();
      console.log(`启动 ${Date.now() - t0}ms | msToken ${signer.state.msToken ? 'ok' : '缺失'}`);
      const t1 = Date.now();
      const r = await signer.detail(args[0] || '7700822507755451647');
      console.log(`详情 ${Date.now() - t1}ms | HTTP ${r.status} | ${r.text.length} 字节`);
      try {
        const j = JSON.parse(r.text);
        const d = j.aweme_detail || {};
        console.log('作者:', (d.author && d.author.nickname) || '-');
        console.log('文案:', String(d.desc || '').slice(0, 80));
        for (const it of pickPlayUrls(d)) console.log(`  ${it.gear}  ${it.size ? Math.round(it.size / 1024) + 'KB' : ''}  ${it.url.slice(0, 100)}...`);
      } catch (e) {
        console.log('解析失败:', e.message, r.text.slice(0, 200));
      }
      process.exit(0);
    })();
  }
}
