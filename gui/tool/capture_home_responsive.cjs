const fs = require("fs"),
  path = require("path");
const { chromium } = require("playwright");
const { PNG } = require("pngjs");
const cases = {
  reference: [400, 690],
  compact: [400, 620],
  tall: [400, 780],
  wide: [760, 590],
  intermediate: [520, 650],
  "before-wide": [759, 690],
  "after-wide": [761, 690],
  large: [1000, 780],
  short: [400, 328],
  narrow: [300, 590],
  long: [400, 328],
};
const mapping = {
  "home-page": '.page[data-page="home"]',
  "home-header": ".home-head",
  "home-status": "#statusBlock",
  "home-mark": "#statusMark",
  "home-context": ".status-context",
  "home-actions": "#actions",
  "home-divider": ".divider",
  "home-notice": "#noticeButton",
};
(async () => {
  const original = path.resolve(process.argv[2]),
    out = path.resolve(process.argv[3]);
  const browser = await chromium.launch({
    headless: true,
    args: ["--no-sandbox"],
    env: process.env,
  });
  const page = await browser.newPage({
    viewport: { width: 1280, height: 1000 },
    deviceScaleFactor: 1,
  });
  const summaries = [];
  for (const [name, [w, h]] of Object.entries(cases)) {
    const viewport = { width: name === "large" ? 1600 : 1280, height: 1000 };
    await page.setViewportSize(viewport);
    await page.goto(require("url").pathToFileURL(original).href);
    for (const [id, value] of [
      [
        "size",
        w >= 760
          ? "wide"
          : name === "compact" || name === "tall"
            ? name
            : "reference",
      ],
      ["scenario", "connected"],
      ["feed", "active"],
      ["themeMode", "light"],
    ])
      await page.selectOption("#" + id, value);
    // Temporary probe dimensions; original HTML bytes remain untouched.
    await page.locator("#window").evaluate(
      (el, { w, h }) => {
        el.style.width = w + "px";
        el.style.height = h + "px";
        const x = el.getBoundingClientRect().x;
        el.style.transform = `translateX(${Math.round(x) - x}px)`;
      },
      { w, h },
    );
    if (name === "long")
      await page.evaluate(() => {
        document.querySelector("#institution").textContent =
          "吉林大学某个很长的校区名称与网络接入机构名称";
        document.querySelector("#username").textContent =
          "student-with-a-very-long-account-name";
        document.querySelector("#statusDetail").textContent = Array(12)
          .fill("认证成功，网络接入信息较长")
          .join("；");
        document.querySelector("#contextNote").textContent = Array(16)
          .fill("以太网适配器及地址的详细说明")
          .join("；");
        document.querySelector(".notice strong").textContent = Array(8)
          .fill("校园网维护安排与相关说明")
          .join("，");
      });
    await page.evaluate(() => document.fonts.ready);
    const measurements = await page.evaluate(
      (selectors) =>
        Object.fromEntries(
          selectors.map((s) => {
            const e = document.querySelector(s),
              r = e.getBoundingClientRect();
            return [s, { x: r.x, y: r.y, width: r.width, height: r.height }];
          }),
        ),
      ["#window", ".view", ...Object.values(mapping)],
    );
    const scroll = await page
      .locator(".view")
      .evaluate((el) => ({
        clientHeight: el.clientHeight,
        scrollHeight: el.scrollHeight,
      }));
    const dir = path.join(out, name);
    fs.mkdirSync(dir, { recursive: true });
    await page
      .locator("#window")
      .screenshot({ path: path.join(dir, "reference-window.png") });
    await page
      .locator(".view")
      .screenshot({ path: path.join(dir, "reference-content.png") });
    fs.writeFileSync(
      path.join(dir, "reference-measurements.json"),
      JSON.stringify({ viewport, dpr: 1, measurements, scroll }, null, 2),
    );
    const f = JSON.parse(
      fs.readFileSync(path.join(dir, "flutter-measurements.json")),
    );
    const a = PNG.sync.read(
        fs.readFileSync(path.join(dir, "reference-content.png")),
      ),
      b = PNG.sync.read(fs.readFileSync(path.join(dir, "flutter-content.png")));
    if (a.width !== b.width || a.height !== b.height)
      throw Error(name + ": canvas mismatch");
    const overlay = new PNG({ width: a.width, height: a.height }),
      diff = new PNG({ width: a.width, height: a.height });
    for (let i = 0; i < a.data.length; i += 4) {
      for (let c = 0; c < 3; c++) {
        overlay.data[i + c] = Math.round((a.data[i + c] + b.data[i + c]) / 2);
        diff.data[i + c] = Math.abs(a.data[i + c] - b.data[i + c]);
      }
      overlay.data[i + 3] = diff.data[i + 3] = 255;
    }
    fs.writeFileSync(path.join(dir, "overlay.png"), PNG.sync.write(overlay));
    fs.writeFileSync(path.join(dir, "difference.png"), PNG.sync.write(diff));
    const view = measurements[".view"];
    let table =
      "| Element | HTML x,y,w,h | Flutter x,y,w,h | delta |\n|---|---|---|---|\n";
    const fmt = (o) =>
      ["x", "y", "width", "height"].map((k) => +o[k].toFixed(3)).join(", ");
    const delta = {};
    for (const [k, s] of Object.entries(mapping)) {
      const hr = measurements[s],
        r = {
          x: hr.x - view.x,
          y: hr.y - view.y,
          width: hr.width,
          height: hr.height,
        };
      const d = Object.fromEntries(
        Object.keys(r).map((p) => [p, f[k][p] - r[p]]),
      );
      delta[k] = d;
      table += `| ${k} | ${fmt(r)} | ${fmt(f[k])} | ${fmt(d)} |\n`;
    }
    fs.writeFileSync(path.join(dir, "geometry-comparison.md"), table);
    summaries.push({
      name,
      outer: [w, h],
      content: [a.width, a.height],
      htmlScroll: scroll,
      flutterScroll: f.scrollExtent,
      delta,
    });
  }
  const old = PNG.sync.read(
    fs.readFileSync(path.resolve("gui/test/candidates/flutter-content.png")),
  );
  const current = PNG.sync.read(
    fs.readFileSync(path.join(out, "reference/flutter-content.png")),
  );
  const changed = old.data.reduce((n, x, i) => n + (x !== current.data[i]), 0);
  fs.writeFileSync(
    path.join(out, "responsive-summary.json"),
    JSON.stringify(
      {
        browser: browser.version(),
        referenceSHA256: require("crypto")
          .createHash("sha256")
          .update(fs.readFileSync(original))
          .digest("hex"),
        defaultChangedChannels: changed,
        cases: summaries,
      },
      null,
      2,
    ),
  );
  if (changed !== 0)
    throw Error("Default candidate pixel regression: " + changed + " channels");
  console.log(
    JSON.stringify({
      cases: summaries.length,
      defaultChangedChannels: changed,
      sizes: summaries
        .slice(0, 4)
        .map((x) => ({ name: x.name, outer: x.outer, content: x.content })),
    }),
  );
  await browser.close();
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
