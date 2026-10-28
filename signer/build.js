// 把签名器打包成「一个 bundle + 四个 secsdk 运行时文件」，供 Go 侧 embed。
//
// 之所以要打包：部署时不想让用户去 npm install 一千多个包，也不希望 Node 冷启动时
// 挨个读文件——某些宿主环境会往 NODE_OPTIONS 塞 --require 钩子包装 fs，逐文件读取会慢
// 出一个数量级（实测 0.3s -> 20s）。打成单个文件后，这个开销只发生一次。
//
// 用法：node build.js
// 产物：../internal/signer/assets/{signer.bundle.js, vendor/*.js}

const esbuild = require('esbuild');
const fs = require('fs');
const path = require('path');

const ROOT = __dirname;
const OUT_DIR = path.resolve(ROOT, '../internal/signer/assets');
const JSDOM = path.join(ROOT, 'node_modules/jsdom');

// jsdom 的 computed-style.js 用 fs.readFileSync(path.resolve(__dirname, ...)) 读一份默认
// 样式表。打包后 __dirname 指到 bundle 所在目录，这行会 ENOENT，所以换成内联字符串。
const stylesheetPath = path.join(JSDOM, 'lib/jsdom/browser/default-stylesheet.css');
const stylesheet = fs.readFileSync(stylesheetPath, 'utf8');

const inlineDefaultStylesheet = {
  name: 'inline-jsdom-stylesheet',
  setup(build) {
    build.onLoad(
      { filter: /jsdom[\\/]lib[\\/]jsdom[\\/]living[\\/]css[\\/]helpers[\\/]computed-style\.js$/ },
      async (args) => {
        const original = await fs.promises.readFile(args.path, 'utf8');
        const patched = original.replace(
          /fs\.readFileSync\(\s*path\.resolve\(__dirname,\s*["']\.\.\/\.\.\/\.\.\/browser\/default-stylesheet\.css["']\),\s*\{[^}]*\}\s*\)/,
          JSON.stringify(stylesheet)
        );
        if (patched === original) {
          // 别静默产出坏 bundle：jsdom 换了写法就立刻失败，让人来更新规则
          throw new Error(
            'computed-style.js 读取默认样式表的方式变了，请更新 build.js 里的替换规则'
          );
        }
        return { contents: patched, loader: 'js' };
      }
    );
  },
};

// jsdom 为同步 XHR 准备了一个 worker 线程，用 require.resolve 定位它。require.resolve
// 不会被 bundler 改写，打包后必然 MODULE_NOT_FOUND；而我们的流程只走异步 XHR（领 msToken），
// 这个 worker 永远不会被拉起，替换成占位字符串即可。
const patchSyncXhrWorker = {
  name: 'patch-xhr-sync-worker',
  setup(build) {
    build.onLoad(
      { filter: /xhr[\\/]XMLHttpRequest-impl\.js$/ },
      async (args) => {
        const original = await fs.promises.readFile(args.path, 'utf8');
        const patched = original.replace(
          /require\.resolve\(\s*["']\.\/xhr-sync-worker\.js["']\s*\)/,
          JSON.stringify('xhr-sync-worker.js')
        );
        if (patched === original) {
          throw new Error(
            'XMLHttpRequest-impl.js 的 worker 引用方式变了，请更新 build.js 里的替换规则'
          );
        }
        return { contents: patched, loader: 'js' };
      }
    );
  },
};

async function main() {
  if (!fs.existsSync(stylesheetPath)) {
    console.error('找不到 jsdom 依赖，请先在 signer/ 目录执行 npm install');
    process.exit(1);
  }

  fs.rmSync(OUT_DIR, { recursive: true, force: true });
  fs.mkdirSync(OUT_DIR, { recursive: true });

  const started = Date.now();
  await esbuild.build({
    entryPoints: [path.join(ROOT, 'signer.js')],
    bundle: true,
    platform: 'node',
    target: 'node20',
    format: 'cjs',
    minify: true,
    legalComments: 'none',
    outfile: path.join(OUT_DIR, 'signer.bundle.js'),
    plugins: [inlineDefaultStylesheet, patchSyncXhrWorker],
    logLevel: 'warning',
  });

  // secsdk 运行时是 win.eval(...) 执行的原始脚本，必须保持原样，不能被 bundler 处理
  const vendorOut = path.join(OUT_DIR, 'vendor');
  fs.mkdirSync(vendorOut, { recursive: true });
  for (const name of fs.readdirSync(path.join(ROOT, 'vendor'))) {
    if (!name.endsWith('.js')) continue;
    fs.copyFileSync(path.join(ROOT, 'vendor', name), path.join(vendorOut, name));
  }

  const bundleSize = fs.statSync(path.join(OUT_DIR, 'signer.bundle.js')).size;
  const vendorSize = fs
    .readdirSync(vendorOut)
    .reduce((sum, name) => sum + fs.statSync(path.join(vendorOut, name)).size, 0);

  console.log(
    `bundle ${(bundleSize / 1024 / 1024).toFixed(2)}MB + vendor ${(vendorSize / 1024).toFixed(0)}KB` +
      ` -> ${path.relative(ROOT, OUT_DIR)} (${Date.now() - started}ms)`
  );
}

main().catch((err) => {
  console.error(err.message || err);
  process.exit(1);
});
