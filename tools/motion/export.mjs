// Renders the narrated Locly film to MP4.
//
// The film (tools/motion/film) is deterministic: Film.seek(t) draws any
// moment exactly, so every frame is captured at its precise time whatever
// the machine's speed. Frames are piped straight into ffmpeg together with
// the voiceover and a sound-effects track synthesized here from the film's
// own cues (key clicks while text types, thuds on the big numbers, …).
//
// Usage (from the repository root):
//   go run ./tools/serve -dir . -port 8790 &
//   node tools/motion/export.mjs --lang en --out docs/assets/video/locly-en.mp4 --poster docs/assets/video/locly-en.jpg
//   node tools/motion/export.mjs --lang ar --out docs/assets/video/locly-ar.mp4 --poster docs/assets/video/locly-ar.jpg
//
// Options: --size 1920x1080 --fps 30 --crf 25 --voice <mp3> --music <file>
//          --no-sfx --poster <jpg> --poster-at <seconds>
//          --video-from <mp4>   keep that file's picture and only redo the sound
// Requires Node 18+, the "playwright" package and ffmpeg (libx264, aac).
// Set CHROMIUM_PATH to use an existing Chromium.
import { chromium } from "playwright";
import { spawn } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const argv = process.argv.slice(2);
const flag = (name) => argv.includes("--" + name);
const opt = (name, def) => {
  const i = argv.indexOf("--" + name);
  return i >= 0 && i + 1 < argv.length ? argv[i + 1] : def;
};

const lang = opt("lang", "en");
const [w, h] = opt("size", "1920x1080").split("x").map(Number);
const fps = Number(opt("fps", 30));
const crf = String(opt("crf", 25));
const base = opt("url", "http://127.0.0.1:8790/tools/motion/film/");
const here = path.dirname(new URL(import.meta.url).pathname);
const voice = opt("voice", path.join(here, "film", "voice", lang + ".mp3"));
const music = opt("music", "");
const out = opt("out", `locly-${lang}.mp4`);
const poster = opt("poster", "");
const videoFrom = opt("video-from", "");

// ------------------------------------------------------------- the frames
const browser = await chromium.launch(process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {});
const page = await browser.newPage({ viewport: { width: w, height: h }, deviceScaleFactor: 1 });
page.on("pageerror", (e) => { console.error(String(e)); process.exit(1); });
await page.goto(`${base}?lang=${lang}`, { waitUntil: "networkidle" });
await page.evaluate(() => window.Film.ready);
const { duration, events } = await page.evaluate(() => ({ duration: window.Film.duration, events: window.Film.sfx() }));
const total = Math.round(duration * fps);

// --------------------------------------------------------- sound effects
const tmp = fs.mkdtempSync(path.join(opt("tmp", os.tmpdir()), "locly-film-"));
const sfxPath = path.join(tmp, "sfx.wav");
writeWav(sfxPath, flag("no-sfx") ? new Float32Array(1) : synthesize(events, duration), 48000);

// ----------------------------------------------------------------- encode
const picture = videoFrom ? ["-i", videoFrom] : ["-f", "image2pipe", "-framerate", String(fps), "-c:v", "mjpeg", "-i", "-"];
const inputs = [...picture, "-i", voice, "-i", sfxPath];
let mix = `[1:a]aresample=48000,apad[v];[2:a]aresample=48000,apad[s];[v][s]amix=inputs=2:normalize=0:duration=first`;
if (music) {
  inputs.push("-stream_loop", "-1", "-i", music);
  mix = `[1:a]aresample=48000,apad[v];[2:a]aresample=48000,apad[s];` +
    `[3:a]aresample=48000,volume=0.18,afade=t=out:st=${(duration - 2).toFixed(2)}:d=2[m];` +
    `[v][s][m]amix=inputs=3:normalize=0:duration=first`;
}
const ff = spawn("ffmpeg", [
  "-hide_banner", "-loglevel", "error", "-y", ...inputs,
  "-filter_complex", mix + ",atrim=0:" + duration.toFixed(3) + ",alimiter=limit=0.94[a]",
  "-map", "0:v", "-map", "[a]",
  ...(videoFrom ? ["-c:v", "copy"] : ["-c:v", "libx264", "-preset", "slow", "-crf", crf, "-tune", "animation", "-pix_fmt", "yuv420p"]),
  "-c:a", "aac", "-b:a", "160k", "-ac", "2",
  "-movflags", "+faststart", "-t", duration.toFixed(3), out,
], { stdio: [videoFrom ? "ignore" : "pipe", "inherit", "inherit"] });
const done = new Promise((resolve, reject) => ff.on("close", (code) => (code === 0 ? resolve() : reject(new Error("ffmpeg exited " + code)))));

for (let i = 0; i < (videoFrom ? 0 : total); i++) {
  await page.evaluate((t) => window.Film.seek(t), i / fps);
  // JPEG at this quality is visually lossless here and ~15× faster to
  // encode than PNG, which would otherwise dominate the render time.
  const frame = await page.screenshot({ type: "jpeg", quality: 95 });
  if (!ff.stdin.write(frame)) await new Promise((r) => ff.stdin.once("drain", r));
  if (i % fps === 0) process.stdout.write(`\r${lang} ${w}x${h}: ${Math.round((i / total) * 100)}%`);
}
if (ff.stdin) ff.stdin.end();
await done;

if (poster) {
  const at = Number(opt("poster-at", duration - 0.4));
  await page.evaluate((t) => window.Film.seek(t), at);
  await page.screenshot({ path: poster, type: "jpeg", quality: 86 });
}
await browser.close();
fs.rmSync(tmp, { recursive: true, force: true });
console.log(`\nwrote ${out}${poster ? " and " + poster : ""}`);

// ======================================================================
// Sound effects. Every sound is synthesized (no samples): short filtered
// noise for clicks, swept sines for thuds and pops, a few partials for
// chimes. A fixed seed keeps renders identical.

function synthesize(list, seconds) {
  const SR = 48000;
  const buf = new Float32Array(Math.ceil((seconds + 2) * SR));
  let seed = 7;
  const rnd = () => (seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0) / 4294967296;

  const env = (t, a, d) => (t < a ? t / a : Math.exp(-(t - a) / d));
  function render(len, fn) {
    const n = Math.round(len * SR);
    const o = new Float32Array(n);
    for (let i = 0; i < n; i++) o[i] = fn(i / SR, i);
    return o;
  }
  // A sine whose frequency glides from f0 to f1 over `glide` seconds.
  function sweep(len, f0, f1, glide, amp, a, d) {
    let ph = 0;
    return render(len, (t) => {
      const f = t < glide ? f0 * Math.pow(f1 / f0, t / glide) : f1;
      ph += (2 * Math.PI * f) / SR;
      return Math.sin(ph) * amp * env(t, a, d);
    });
  }
  function noise(len, amp, d, hp) {
    let prevIn = 0, prevOut = 0;
    return render(len, (t) => {
      const x = rnd() * 2 - 1;
      prevOut = hp ? 0.9 * (prevOut + x - prevIn) : 0.6 * prevOut + 0.4 * x;
      prevIn = x;
      return prevOut * amp * Math.exp(-t / d);
    });
  }
  function bell(len, freqs, amps, decays) {
    return render(len, (t) => {
      let v = 0;
      for (let k = 0; k < freqs.length; k++) v += Math.sin(2 * Math.PI * freqs[k] * t) * amps[k] * env(t, 0.002, decays[k]);
      return v;
    });
  }
  function mixInto(t0, s, gain = 1) {
    const i0 = Math.round(t0 * SR);
    for (let i = 0; i < s.length; i++) {
      const j = i0 + i;
      if (j >= 0 && j < buf.length) buf[j] += s[i] * gain;
    }
  }
  // Band-passed noise whose centre sweeps up: an air "whoosh".
  function whoosh(len, f0, f1, amp) {
    let low = 0, band = 0;
    return render(len, (t) => {
      const x = rnd() * 2 - 1;
      const f = f0 * Math.pow(f1 / f0, t / len);
      const k = 2 * Math.sin((Math.PI * f) / SR);
      low += k * band;
      const high = x - low - 0.35 * band;
      band += k * high;
      const e = Math.pow(Math.sin(Math.PI * Math.min(1, t / len)), 2);
      return band * amp * e;
    });
  }

  const make = {
    key: () => {
      const f = 1900 + rnd() * 1500;
      const a = 0.11 + rnd() * 0.05;
      const n = noise(0.04, a, 0.004, true);
      const tone = bell(0.04, [f, 190 + rnd() * 40], [a * 0.35, a * 0.4], [0.0025, 0.012]);
      return n.map((v, i) => v + tone[i]);
    },
    click: () => {
      const one = (amp) => {
        const n = noise(0.03, amp, 0.003, true);
        const tone = bell(0.03, [2600, 900], [amp * 0.5, amp * 0.3], [0.002, 0.006]);
        return n.map((v, i) => v + tone[i]);
      };
      const o = new Float32Array(Math.round(0.12 * SR));
      one(0.24).forEach((v, i) => (o[i] += v));
      one(0.12).forEach((v, i) => (o[i + Math.round(0.075 * SR)] += v));
      return o;
    },
    pop: () => sweep(0.2, 760 + rnd() * 120, 300, 0.08, 0.16, 0.003, 0.05),
    thud: () => {
      const body = sweep(0.45, 150, 46, 0.22, 0.34, 0.002, 0.16);
      const hit = noise(0.05, 0.12, 0.012, false);
      return body.map((v, i) => v + (hit[i] || 0));
    },
    stamp: () => {
      const body = sweep(0.3, 120, 50, 0.12, 0.36, 0.001, 0.09);
      const slap = noise(0.06, 0.2, 0.015, true);
      return body.map((v, i) => v + (slap[i] || 0));
    },
    boom: () => {
      const body = sweep(1.4, 120, 36, 0.6, 0.32, 0.004, 0.45);
      const air = whoosh(0.6, 2500, 300, 0.05);
      const shimmer = bell(1.4, [1568, 2093, 2637], [0.025, 0.02, 0.014], [0.9, 0.7, 0.5]);
      return body.map((v, i) => v + (air[i] || 0) + shimmer[i]);
    },
    whoosh: () => whoosh(0.55, 280, 2600, 0.16),
    swish: () => {
      const o = whoosh(0.5, 900, 4200, 0.06);
      for (let k = 0; k < 16; k++) {
        const blip = bell(0.03, [1200 + rnd() * 2400], [0.035], [0.008]);
        blip.forEach((v, i) => {
          const j = Math.round(k * 0.028 * SR) + i;
          if (j < o.length) o[j] += v;
        });
      }
      return o;
    },
    ding: () => bell(1.6, [1318.5, 1975.5, 2637], [0.09, 0.045, 0.025], [1.1, 0.7, 0.4]),
    chime: () => {
      const o = new Float32Array(Math.round(2.2 * SR));
      [1046.5, 1318.5, 1568, 2093].forEach((f, k) => {
        bell(1.8, [f, f * 2], [0.06, 0.012], [1.2, 0.5]).forEach((v, i) => (o[i + Math.round(k * 0.085 * SR)] += v));
      });
      return o;
    },
  };

  for (const ev of list) {
    if (!make[ev.kind]) throw new Error("unknown sound: " + ev.kind);
    // Whooshes peak at the cue, so they start a little before it.
    const lead = ev.kind === "whoosh" ? 0.3 : 0;
    mixInto(ev.t - lead, make[ev.kind](), 0.62);
  }
  return buf;
}

function writeWav(file, samples, rate) {
  const data = Buffer.alloc(samples.length * 2);
  for (let i = 0; i < samples.length; i++) {
    const v = Math.max(-1, Math.min(1, samples[i]));
    data.writeInt16LE(Math.round(v * 32767), i * 2);
  }
  const head = Buffer.alloc(44);
  head.write("RIFF", 0);
  head.writeUInt32LE(36 + data.length, 4);
  head.write("WAVE", 8);
  head.write("fmt ", 12);
  head.writeUInt32LE(16, 16);
  head.writeUInt16LE(1, 20);
  head.writeUInt16LE(1, 22);
  head.writeUInt32LE(rate, 24);
  head.writeUInt32LE(rate * 2, 28);
  head.writeUInt16LE(2, 32);
  head.writeUInt16LE(16, 34);
  head.write("data", 36);
  head.writeUInt32LE(data.length, 40);
  fs.writeFileSync(file, Buffer.concat([head, data]));
}
