// Record the compiled Flutter app, using accessibility controls from its actual UI.
const fs = require("fs"),
  path = require("path"),
  http = require("http");
const { chromium } = require("playwright");
(async () => {
  const root = path.resolve(process.argv[2]),
    out = path.resolve(process.argv[3]);
  fs.mkdirSync(out, { recursive: true });
  const symbol =
    process.env.HOME_SYMBOL_FONT || "/mnt/c/Windows/Fonts/seguisym.ttf";
  const manifestPath = path.join(root, "assets/FontManifest.json");
  if (fs.existsSync(symbol)) {
    const manifest = JSON.parse(fs.readFileSync(manifestPath));
    if (!manifest.some((f) => f.family === "Segoe UI Symbol")) {
      manifest.push({
        family: "Segoe UI Symbol",
        fonts: [{ asset: "fonts/seguisym.ttf" }],
      });
      fs.writeFileSync(manifestPath, JSON.stringify(manifest));
      if (!fs.existsSync(path.join(root, "assets/fonts/seguisym.ttf")))
        fs.copyFileSync(symbol, path.join(root, "assets/fonts/seguisym.ttf"));
    }
  }
  const bootstrap = path.join(root, "flutter_bootstrap.js");
  if (fs.existsSync(path.join(root, "font-fallback"))) {
    const source = fs.readFileSync(bootstrap, "utf8");
    if (!source.includes("fontFallbackBaseUrl"))
      fs.writeFileSync(
        bootstrap,
        source.replace(
          "_flutter.loader.load({",
          '_flutter.loader.load({config:{fontFallbackBaseUrl:new URL("font-fallback/",window.location.href).href},',
        ),
      );
  }
  const server = http.createServer((req, res) => {
    const pathname = new URL(req.url, "http://localhost").pathname;
    const f = path.resolve(
      root,
      "." + decodeURIComponent(pathname === "/" ? "/index.html" : pathname),
    );
    if (!f.startsWith(root + path.sep)) {
      res.writeHead(403);
      res.end();
      return;
    }
    fs.readFile(f, (error, body) => {
      if (error) {
        res.writeHead(404);
        res.end();
        return;
      }
      const mime = {
        ".html": "text/html",
        ".js": "application/javascript",
        ".wasm": "application/wasm",
        ".json": "application/json",
        ".ttf": "font/ttf",
        ".otf": "font/otf",
        ".png": "image/png",
      };
      res.writeHead(200, {
        "Content-Type": mime[path.extname(f)] || "application/octet-stream",
        "Cache-Control": "no-cache",
      });
      res.end(body);
    });
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const url = `http://127.0.0.1:${server.address().port}/`;
  const browser = await chromium.launch({
    headless: true,
    args: ["--no-sandbox"],
    env: process.env,
  });
  const context = await browser.newContext({
    viewport: { width: 398, height: 698 },
    deviceScaleFactor: 1,
    recordVideo: { dir: out, size: { width: 1100, height: 900 } },
  });
  const blocked = [],
    errors = [],
    steps = [];
  await context.route("**/*", (route) => {
    const u = route.request().url();
    if (u.startsWith(url) || u.startsWith("data:") || u.startsWith("blob:"))
      return route.continue();
    blocked.push(u);
    return route.abort();
  });
  const page = await context.newPage();
  page.on("pageerror", (e) => errors.push(String(e)));
  await page.goto(url);
  await page.waitForSelector("flt-semantics-placeholder", { timeout: 30000 });
  await page.locator("flt-semantics-placeholder").evaluate((e) => e.click());
  await page
    .getByText("离线演示 · 勿输入真实账号或密码", { exact: true })
    .waitFor();
  await page.screenshot({ path: path.join(out, "interactive-initial.png") });
  console.log("buttons", await page.getByRole("button").allTextContents());
  const step = async (name, action) => {
    await action();
    await page.waitForTimeout(550);
    steps.push({
      name,
      time: Date.now(),
      viewport: page.viewportSize(),
      visibleText: await page.locator("flt-semantics-host").innerText(),
    });
    await page.screenshot({
      path: path.join(out, `${String(steps.length).padStart(2, "0")}.png`),
    });
  };
  if (process.argv[4] !== "--probe") {
    await page.waitForTimeout(900);
    await step("open configuration", () =>
      page.getByRole("button", { name: /吉林大学\s*student01/ }).click(),
    );
    await page.getByRole("textbox").first().click();
    await page.waitForTimeout(150);
    await page.keyboard.press("End");
    for(let i=0;i<9;i++) await page.keyboard.press("Backspace");
    await page.keyboard.type("demo-student02");
    await step("save configuration", () =>
      page.getByRole("button", { name: "保存更改", exact: true }).click(),
    );
    await page
      .getByRole("button", { name: /吉林大学\s*demo-student02/ })
      .waitFor();
    await step("open settings", () =>
      page.getByRole("button", { name: "设置", exact: true }).click(),
    );
    await step("back from settings", () =>
      page.getByRole("button", { name: "返回连接", exact: true }).click(),
    );
    await step("open connection details", () =>
      page.getByRole("button", { name: "连接详情", exact: true }).click(),
    );
    await step("open diagnostics", () =>
      page.getByRole("button", { name: /^技术诊断/ }).click(),
    );
    await step("back to details", () =>
      page.getByRole("button", { name: "返回", exact: true }).click(),
    );
    await step("back to home", () =>
      page.getByRole("button", { name: "返回", exact: true }).click(),
    );
    await step("open announcement", () =>
      page.getByRole("button", { name: /校园网公告.*校园网维护安排/s }).click(),
    );
    await page.getByText(/这是离线演示公告/).waitFor();
    await step("dismiss announcement", () => page.keyboard.press("Escape"));
    await step("simulate disconnect", () =>
      page.getByRole("button", { name: "断开连接", exact: true }).click(),
    );
    await page
      .getByText("已模拟断开连接；没有影响真实网络", { exact: true })
      .waitFor();
    await step("simulate reconnect", () =>
      page.getByRole("button", { name: "开始连接", exact: true }).click(),
    );
    await page
      .getByText("已模拟连接成功；没有进行真实认证", { exact: true })
      .waitFor();
    for (const label of ['正在认证','等待网络','等待重试','认证失败','尚未配置','已连接']) {
      await step('choose demo ' + label, async () => {
        await page.getByRole('button', {name: /场景：/}).click();
        await page.waitForTimeout(350);
        await page.screenshot({path:path.join(out, 'menu-'+label+'.png')});
        // Popup semantics vanish in this web engine; coordinates were observed in menu-probe screenshots at DPR1.
        const index={'已连接':0,'正在认证':2,'等待网络':3,'等待重试':4,'认证失败':5,'尚未配置':6}[label];
        await page.mouse.click(60,69+index*48);
        await page.getByText(label,{exact:true}).first().waitFor();
      });
    }
    await step('demo dark theme', async () => {
      await page.getByRole('button', {name:/演示外观/}).click();
      await page.waitForTimeout(350);
      await page.mouse.click(60,197);
    });
    await step('demo light theme', async () => {
      await page.getByRole('button', {name:/演示外观/}).click();
      await page.waitForTimeout(350);
      await page.mouse.click(60,149);
    });
    await step('hide demo announcement', () => page.getByRole('button',{name:'隐藏公告（模拟）',exact:true}).click());
    await step('show demo announcement', () => page.getByRole('button',{name:'显示公告（模拟）',exact:true}).click());
    await step('update demo announcement', () => page.getByRole('button',{name:'更新公告（模拟）',exact:true}).click());
    for (const [targetW, targetH] of [
      [758, 598],
      [998, 788],
      [518, 658],
      [398, 336],
      [398, 788],
      [398, 698],
    ]) {
      const from = page.viewportSize();
      for (let n = 1; n <= 24; n++) {
        await page.setViewportSize({
          width: Math.round(from.width + ((targetW - from.width) * n) / 24),
          height: Math.round(from.height + ((targetH - from.height) * n) / 24),
        });
        await page.waitForTimeout(60);
      }
      await step(`resize to ${targetW}x${targetH}`, async () => {});
      if (targetH === 336) {
        await page.mouse.move(200, 260);
        await step("scroll short viewport", () => page.mouse.wheel(0, 600));
        await page.mouse.wheel(0, -600);
      }
    }
  }
  const video = await page.video().path();
  await context.close();
  const movie = path.join(
    out,
    process.argv[4] === "--probe" ? "probe.webm" : "interactive-demo.webm",
  );
  fs.renameSync(video, movie);
  fs.writeFileSync(
    path.join(out, "recording.json"),
    JSON.stringify(
      {
        platform:
          "WSL Linux Chromium; actual compiled Flutter UI with offline injection; not Windows native",
        steps,
        blockedExternalRequests: blocked,
        pageErrors: errors,
        video: movie,
      },
      null,
      2,
    ),
  );
  console.log(
    JSON.stringify({
      movie,
      steps: steps.length,
      blockedExternalRequests: blocked,
      pageErrors: errors,
    }),
  );
  await browser.close();
  server.close();
  if (errors.length || blocked.length) process.exitCode = 1;
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
