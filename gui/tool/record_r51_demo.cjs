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
    .getByRole("button", { name: "设置", exact: true })
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
    const sweep = async (name) => {
      const samples=[];
      for(let width=360;width<=1000;width+=(width>=748&&width<=770)||(width>=948&&width<=972)?1:4){
        await page.setViewportSize({width,height:698});
        await page.waitForTimeout(35);
        samples.push({width,buttonRects:await page.getByRole('button').evaluateAll(es=>es.slice(0,12).map(e=>({text:e.innerText,x:e.getBoundingClientRect().x,y:e.getBoundingClientRect().y,w:e.getBoundingClientRect().width,h:e.getBoundingClientRect().height})))});
      }
      fs.writeFileSync(path.join(out,name+'-sweep.json'),JSON.stringify(samples,null,2));
      await step(name,async()=>{});
    };
    await sweep('home-continuous');
    await page.getByRole('button',{name:'设置',exact:true}).click();
    await sweep('settings-continuous');
    await step('short settings scroll',async()=>{
      await page.setViewportSize({width:360,height:320});
      await page.mouse.move(180,250);await page.mouse.wheel(0,600);
    });
    await page.setViewportSize({width:398,height:698});
    await page.getByRole('button',{name:'返回连接',exact:true}).click();
    await step('PC narrow dialog',()=>page.getByRole('button',{name:/校园网公告.*校园网维护安排/s}).click());
    await step('PC Escape close',()=>page.keyboard.press('Escape'));
    await step('developer mobile presentation',()=>page.getByRole('button',{name:'公告预览：PC 对话框',exact:true}).click());
    await step('mobile sheet mouse preview',()=>page.getByRole('button',{name:/校园网公告.*校园网维护安排/s}).click());
    await step('sheet close',()=>page.getByRole('button',{name:'关闭公告',exact:true}).click());
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
