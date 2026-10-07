const fs = require("fs"),
  { PNG } = require("pngjs");
const out = require("path").resolve(process.argv[2] || "test/candidates");
const a = PNG.sync.read(fs.readFileSync(out + "/reference-content.png")),
  b = PNG.sync.read(fs.readFileSync(out + "/flutter-content.png"));
if (a.width !== b.width || a.height !== b.height)
  throw Error("canvas mismatch");
const overlay = new PNG({ width: a.width, height: a.height }),
  diff = new PNG({ width: a.width, height: a.height });
for (let i = 0; i < a.data.length; i += 4) {
  for (let c = 0; c < 3; c++) {
    overlay.data[i + c] = Math.round((a.data[i + c] + b.data[i + c]) / 2);
    diff.data[i + c] = Math.abs(a.data[i + c] - b.data[i + c]);
  }
  overlay.data[i + 3] = diff.data[i + 3] = 255;
}
fs.writeFileSync(out + "/overlay.png", PNG.sync.write(overlay));
fs.writeFileSync(out + "/difference.png", PNG.sync.write(diff));
const r = JSON.parse(fs.readFileSync(out + "/reference-measurements.json")),
  f = JSON.parse(fs.readFileSync(out + "/flutter-measurements.json"));
const mapping = {
  "home-header": ".home-head",
  "home-status": "#statusBlock",
  "home-mark": "#statusMark",
  "home-context": ".status-context",
  "home-actions": "#actions",
  "home-divider": ".divider",
  "home-notice": "#noticeButton",
  institution: "#institution",
  username: "#username",
  "status-title": "#statusTitle",
  "status-detail": "#statusDetail",
  "notice-meta": ".notice-meta",
  "notice-title": ".notice strong",
  "notice-description": ".notice p",
};
let report =
  "| 元素 | HTML x,y,w,h | Flutter x,y,w,h | dx,dy,dw,dh |\n|---|---|---|---|\n";
for (const [k, s] of Object.entries(mapping)) {
  const h = r.measurements[s],
    v = r.measurements[".view"];
  const rect = { x: h.x - v.x, y: h.y - v.y, width: h.width, height: h.height };
  const ff = f[k];
  let fmt = (o) =>
    ["x", "y", "width", "height"]
      .map((p) => Number(o[p].toFixed(3)))
      .join(", ");
  const d = Object.fromEntries(
    Object.keys(rect).map((p) => [p, ff[p] - rect[p]]),
  );
  report += `| ${k} | ${fmt(rect)} | ${fmt(ff)} | ${fmt(d)} |\n`;
}
fs.writeFileSync(out + "/geometry-comparison.md", report);
console.log(report);
