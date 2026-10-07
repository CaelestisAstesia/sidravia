const { chromium } = require("playwright");
const fs = require("fs");
(async () => {
  const browser = await chromium.launch({
    headless: true,
    args: ["--no-sandbox"],
    env: process.env,
  });
  const page = await browser.newPage({
    viewport: { width: 1280, height: 1000 },
    deviceScaleFactor: 1,
  });
  await page.goto(
    require("url").pathToFileURL(require("path").resolve(process.argv[2])).href,
  );
  for (const [id, value] of [
    ["size", "reference"],
    ["scenario", "connected"],
    ["feed", "active"],
    ["themeMode", "light"],
  ])
    await page.selectOption("#" + id, value);
  await page.evaluate(() => document.fonts.ready);
  const measurements = await page.evaluate(() =>
    Object.fromEntries(
      [
        "#window",
        ".view",
        ".home-head",
        "#institution",
        "#username",
        "#statusBlock",
        "#statusMark",
        "#statusTitle",
        "#statusDetail",
        ".status-context",
        "#contextNote",
        "#actions",
        "#secondaryAction",
        "#primaryAction",
        ".divider",
        "#noticeButton",
        ".notice-meta",
        ".notice strong",
        ".notice p",
        ".notice .arrow",
      ].map((s) => {
        const el = document.querySelector(s),
          r = el.getBoundingClientRect(),
          c = getComputedStyle(el);
        return [
          s,
          {
            x: r.x,
            y: r.y,
            width: r.width,
            height: r.height,
            font: c.font,
            color: c.color,
          },
        ];
      }),
    ),
  );
  const out = require("path").resolve(process.argv[3] || "test/candidates");
  fs.mkdirSync(out, { recursive: true });
  await page
    .locator("#window")
    .screenshot({ path: out + "/reference-window.png" });
  await page
    .locator(".view")
    .screenshot({ path: out + "/reference-content.png" });
  const session = await page.context().newCDPSession(page);
  await session.send("DOM.enable");
  await session.send("CSS.enable");
  const fonts = {};
  for (const sel of [
    "#institution",
    "#username",
    "#statusTitle",
    "#statusGlyph",
  ]) {
    const doc = await session.send("DOM.getDocument");
    const node = await session.send("DOM.querySelector", {
      nodeId: doc.root.nodeId,
      selector: sel,
    });
    fonts[sel] = await session.send("CSS.getPlatformFontsForNode", {
      nodeId: node.nodeId,
    });
  }
  fs.writeFileSync(
    out + "/reference-measurements.json",
    JSON.stringify(
      {
        browser: browser.version(),
        viewport: { width: 1280, height: 1000 },
        dpr: 1,
        measurements,
        fonts,
      },
      null,
      2,
    ),
  );
  console.log(
    JSON.stringify({
      browser: browser.version(),
      view: measurements[".view"],
      fonts,
    }),
  );
  await browser.close();
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
