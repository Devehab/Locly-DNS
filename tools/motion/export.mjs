// Renders the landing-page story animation to an MP4, frame by frame.
//
// The animation is deterministic (LoclyMotion.seek(t) draws any moment
// exactly), so every frame is captured at the precise time regardless of
// machine speed.
//
// Usage (from the repository root, with the site served locally):
//   go run ./tools/serve -dir docs -port 8790 &
//   node tools/motion/export.mjs --lang ar --size 1920x1080 --out locly-ar-16x9.mp4
//
// Requires Node 18+, the "playwright" package and ffmpeg with libx264.
import { chromium } from "playwright";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const args = Object.fromEntries(
  process.argv.slice(2).reduce((acc, a, i, all) => (a.startsWith("--") ? [...acc, [a.slice(2), all[i + 1]]] : acc), [])
);
const lang = args.lang || "en";
const [w, h] = (args.size || "1920x1080").split("x").map(Number);
const fps = Number(args.fps || 30);
const url = args.url || "http://127.0.0.1:8790/";
const out = args.out || `locly-${lang}-${w}x${h}.mp4`;

const frames = fs.mkdtempSync(path.join(os.tmpdir(), "locly-frames-"));
const browser = await chromium.launch(
  process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {}
);
const page = await browser.newPage({ viewport: { width: w, height: h }, deviceScaleFactor: 1 });
await page.goto(`${url}?lang=${lang}`, { waitUntil: "networkidle" });
await page.evaluate(() => document.fonts.ready);
// Make the stage fill the frame. Its container queries pick the right layout
// for the aspect ratio (side by side for 16:9, stacked for square).
await page.addStyleTag({
  content:
    "html,body{overflow:hidden!important}.site-header{display:none!important}" +
    ".motion-stage{position:fixed!important;inset:0!important;width:100vw!important;height:100vh!important;" +
    "aspect-ratio:auto!important;border-radius:0!important;border:0!important;box-shadow:none!important;z-index:2147483647}",
});

const duration = await page.evaluate(() => window.LoclyMotion.duration);
const total = Math.round(duration * fps);
for (let i = 0; i < total; i++) {
  await page.evaluate((t) => window.LoclyMotion.seek(t), i / fps);
  await page.screenshot({ path: path.join(frames, `f_${String(i).padStart(5, "0")}.png`) });
  if (i % fps === 0) process.stdout.write(`\r${lang} ${w}x${h}: ${Math.round((i / total) * 100)}%`);
}
await browser.close();

execFileSync("ffmpeg", [
  "-hide_banner", "-loglevel", "error", "-y",
  "-framerate", String(fps), "-i", path.join(frames, "f_%05d.png"),
  "-c:v", "libx264", "-preset", "slow", "-crf", "18", "-pix_fmt", "yuv420p", "-movflags", "+faststart",
  out,
]);
fs.rmSync(frames, { recursive: true, force: true });
console.log(`\nwrote ${out}`);
