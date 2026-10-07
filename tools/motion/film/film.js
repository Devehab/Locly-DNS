// Locly narrated film: one composition, two voiceovers (English and Arabic).
//
// Every moment is a function of the time t, so any frame can be drawn
// exactly with Film.seek(t); export.mjs captures frames that way and lays
// the voiceover under them. Times come from the voiceovers themselves
// (speech-recognition word timings, checked against the audio's loudness),
// so each word and visual lands on the word that is spoken.
//
// Markup vocabulary (times are expressions: a number, a cue name, or
// cue±seconds, e.g. "n127", "ready+0.3", "12.5"):
//   class="scene" data-in data-out    fades the scene in and out; sets --in, --out, --sp
//   data-v="var@time/dur[:ease] …"     eased 0→1 progress in --var
//   data-words="time time …"           one entry per word; each word gets --p
//   data-type="time/dur" data-text     types the text
//   data-scramble="time/dur" data-from data-text   decodes one text into another
//   data-k="var: time=value, time=value"           keyframes (eased) in --var
//   data-shake="time time …"           camera hits; sets --shx/--shy on the scene
//   data-sfx="kind@time …"             sound effects (read by the exporter)
(function () {
  "use strict";

  var CUES = {
    en: {
      audio: 50.91,
      nums: 2.92, n127: 4.0, n0a: 4.78, n0b: 5.1, n1: 5.48, port: 6.05, n3000: 6.35,
      why: 7.32, harder: 8.8,
      with: 10.5, locly: 10.68, oneLine: 12.2,
      install: 13.5, typed: 14.87, ready: 15.8,
      then: 16.8, open: 17.1, add: 18.7, hostType: 19.25, addrType: 19.85,
      give: 20.08, name: 20.94, submit: 21.4, remember: 21.94,
      instead: 22.98, i127: 23.66, i0a: 24.64, i0b: 24.95, i1: 25.5, iport: 25.75, i3000: 25.8,
      youget: 26.38, app: 26.76, local: 27.12,
      no1: 27.58, registration: 28.56, no2: 29.52, setup: 30.75,
      more: 31.9, developer: 33.36, alsoGives: 34.15, terminal: 35.42,
      cAdd: 37.5, cEdit: 38.04, cEnable: 38.52, cRemove: 39.15, simple: 40.28,
      list: 41.45, json: 42.12, automation: 42.7, ai: 43.5,
      stays: 44.7, vLocal: 45.45, vSimple: 46.25, vOrganized: 46.9,
      final: 48.1, names: 49.25, not: 50.02, numbers: 50.12
    },
    ar: {
      audio: 53.71,
      nums: 3.88, n127: 5.22, n0a: 6.05, n0b: 6.48, n1: 6.98, port: 7.95, n3000: 8.5,
      why: 9.86, harder: 10.54,
      with: 11.3, locly: 11.5, oneLine: 13.04,
      install: 14.5, typed: 15.9, ready: 16.55,
      then: 17.3, open: 18.35, add: 19.86, hostType: 20.45, addrType: 21.95,
      give: 21.46, name: 23.22, submit: 23.62, remember: 23.9,
      instead: 24.8, i127: 25.2, i0a: 26.28, i0b: 26.72, i1: 27.1, iport: 28.32, i3000: 28.75,
      youget: 29.7, app: 30.5, local: 30.88,
      no1: 31.5, registration: 32.45, no2: 33.1, setup: 34.3,
      more: 35.3, developer: 36.56, alsoGives: 37.18, terminal: 38.7,
      cAdd: 40.05, cEdit: 40.55, cEnable: 41.22, cRemove: 41.75, simple: 42.7,
      list: 44.05, json: 44.6, automation: 45.3, ai: 45.85,
      stays: 47.76, vLocal: 48.25, vSimple: 49.05, vOrganized: 49.75,
      final: 50.22, names: 51.6, not: 52.45, numbers: 52.75
    }
  };
  var TAIL = 2.0; // the end card stays after the last word
  var FADE = 0.45; // scene cross-fade
  var WORD = 0.36; // one word's entrance

  var params = new URLSearchParams(location.search);
  var lang = params.get("lang") === "ar" ? "ar" : "en";
  var root = document.documentElement;
  root.lang = lang;
  root.dir = lang === "ar" ? "rtl" : "ltr";

  var cues = CUES[lang];
  var duration = +(cues.audio + TAIL).toFixed(2);
  cues.end = duration;

  // ---------------------------------------------------------------- helpers
  var ease = {
    out: function (p) { return 1 - Math.pow(1 - p, 3); },
    io: function (p) { return p < 0.5 ? 4 * p * p * p : 1 - Math.pow(-2 * p + 2, 3) / 2; },
    back: function (p) { var c = 1.70158; return 1 + (c + 1) * Math.pow(p - 1, 3) + c * Math.pow(p - 1, 2); },
    soft: function (p) { var c = 0.9; return 1 + (c + 1) * Math.pow(p - 1, 3) + c * Math.pow(p - 1, 2); },
    expo: function (p) { return p >= 1 ? 1 : 1 - Math.pow(2, -10 * p); },
    lin: function (p) { return p; }
  };
  function clamp(v) { return v < 0 ? 0 : v > 1 ? 1 : v; }

  function time(expr) {
    expr = String(expr).trim();
    var m = /^([A-Za-z]\w*)?\s*([+-]\s*[\d.]+)?$/.exec(expr);
    if (m && (m[1] || m[2])) {
      var base = 0;
      if (m[1]) {
        if (!(m[1] in cues)) throw new Error("unknown cue: " + m[1] + " (" + lang + ")");
        base = cues[m[1]];
      }
      return base + (m[2] ? parseFloat(m[2].replace(/\s+/g, "")) : 0);
    }
    var n = parseFloat(expr);
    if (isNaN(n)) throw new Error("bad time: " + expr);
    return n;
  }

  // Elements written for the other language are left alone.
  var other = lang === "ar" ? ".en" : ".ar";
  function live(el) { return !el.closest(other); }
  function all(sel, scope) {
    return Array.prototype.filter.call((scope || document).querySelectorAll(sel), live);
  }

  // A tiny deterministic hash, so "random" motion is identical every render.
  function hash(a, b) {
    var h = (a * 374761393 + b * 668265263) | 0;
    h = (h ^ (h >>> 13)) * 1274126177;
    return ((h ^ (h >>> 16)) >>> 0) / 4294967296;
  }

  // ------------------------------------------------------------- build words
  // Split each data-words element into word spans. Child elements (such as a
  // gradient <b>) count as one word each.
  all("[data-words]").forEach(function (el) {
    var times = el.getAttribute("data-words").trim().split(/\s+/);
    var tokens = [];
    Array.prototype.slice.call(el.childNodes).forEach(function (node) {
      if (node.nodeType === 3) {
        var frag = document.createDocumentFragment();
        node.textContent.split(/(\s+)/).forEach(function (part) {
          if (!part) return;
          if (/^\s+$/.test(part)) { frag.appendChild(document.createTextNode(part)); return; }
          var span = document.createElement("span");
          span.className = "wd";
          span.textContent = part;
          frag.appendChild(span);
          tokens.push(span);
        });
        el.replaceChild(frag, node);
      } else if (node.nodeType === 1 && node.tagName !== "BR") {
        node.classList.add("wd");
        tokens.push(node);
      }
    });
    if (tokens.length !== times.length) {
      throw new Error("data-words has " + times.length + " times for " + tokens.length + " words: " + el.textContent);
    }
    tokens.forEach(function (tok, i) {
      var own = tok.getAttribute("data-v");
      tok.setAttribute("data-v", (own ? own + " " : "") + "p@" + times[i] + "/" + WORD + ":soft");
    });
  });

  // ------------------------------------------------------------------ parse
  function parseSpecs(str) {
    return str.trim().split(/\s+/).map(function (part) {
      var m = /^([\w-]+)@([^/]+)\/([\d.]+)(?::(\w+))?$/.exec(part);
      if (!m) throw new Error("bad data-v: " + part);
      return { name: "--" + m[1], at: time(m[2]), dur: +m[3], ease: ease[m[4] || "out"] };
    });
  }

  function parseKeys(str) {
    return str.split(";").map(function (track) {
      var m = /^\s*([\w-]+)\s*:(.*)$/.exec(track);
      if (!m) throw new Error("bad data-k: " + track);
      var keys = m[2].split(",").map(function (kv) {
        var p = kv.split("=");
        return { t: time(p[0]), v: parseFloat(p[1]) };
      });
      return { name: "--" + m[1], keys: keys };
    });
  }

  var stage = document.getElementById("stage");
  var scenes = all(".scene").map(function (el) {
    return { el: el, tin: time(el.getAttribute("data-in")), tout: time(el.getAttribute("data-out")) };
  });
  function sceneOf(el) {
    var s = el.closest(".scene");
    for (var i = 0; i < scenes.length; i++) if (scenes[i].el === s) return scenes[i];
    return null;
  }
  function attach(list) {
    list.forEach(function (it) {
      var sc = sceneOf(it.el);
      (sc ? (sc.items = sc.items || []) : globals).push(it);
    });
  }
  var globals = [];

  attach(all("[data-v]").map(function (el) {
    return { kind: "v", el: el, specs: parseSpecs(el.getAttribute("data-v")) };
  }));
  attach(all("[data-k]").map(function (el) {
    return { kind: "k", el: el, tracks: parseKeys(el.getAttribute("data-k")) };
  }));
  // "start/duration" or "start>end"
  function span(str) {
    var r = str.split(">");
    if (r.length === 2) { var a = time(r[0]); return { at: a, dur: time(r[1]) - a }; }
    var p = str.split("/");
    return { at: time(p[0]), dur: +p[1] };
  }
  attach(all("[data-type]").map(function (el) {
    var s = span(el.getAttribute("data-type"));
    return { kind: "type", el: el, at: s.at, dur: s.dur, text: el.getAttribute("data-text") || "", shown: -1 };
  }));
  attach(all("[data-path]").map(function (el) {
    var keys = el.getAttribute("data-path").trim().split(/\s+/).map(function (kv) {
      var p = kv.split("=");
      return { t: time(p[0]), to: p[1] };
    });
    return { kind: "path", el: el, keys: keys, pts: null };
  }));
  attach(all("[data-scramble]").map(function (el) {
    var s = span(el.getAttribute("data-scramble"));
    return {
      kind: "scramble", el: el, at: s.at, dur: s.dur,
      from: el.getAttribute("data-from") || "", text: el.getAttribute("data-text") || "", shown: null
    };
  }));
  scenes.forEach(function (sc) {
    sc.items = sc.items || [];
    var hits = sc.el.getAttribute("data-shake");
    sc.hits = hits ? hits.trim().split(/\s+/).map(time) : [];
  });

  // A path point is "x,y" on the stage, or "#id" for the middle of an
  // element (its layout position: transforms in flight are ignored).
  function point(to) {
    if (to.charAt(0) !== "#") return to.split(",").map(Number);
    var el = document.getElementById(to.slice(1));
    if (!el) throw new Error("no element " + to);
    var x = el.offsetWidth / 2, y = el.offsetHeight / 2;
    for (var n = el; n && n !== stage; n = n.offsetParent) { x += n.offsetLeft; y += n.offsetTop; }
    return [x, y];
  }

  // ----------------------------------------------------------------- render
  var GLYPHS = "0123456789abcdef.:/-_";

  function renderItem(it, t) {
    if (it.kind === "v") {
      it.specs.forEach(function (sp) {
        var p = sp.dur > 0 ? clamp((t - sp.at) / sp.dur) : (t >= sp.at ? 1 : 0);
        it.el.style.setProperty(sp.name, sp.ease(p).toFixed(4));
      });
    } else if (it.kind === "k") {
      it.tracks.forEach(function (tr) {
        var k = tr.keys, v = k[0].v;
        if (t >= k[k.length - 1].t) v = k[k.length - 1].v;
        else for (var i = 0; i < k.length - 1; i++) {
          if (t >= k[i].t && t < k[i + 1].t) {
            v = k[i].v + (k[i + 1].v - k[i].v) * ease.io((t - k[i].t) / (k[i + 1].t - k[i].t));
            break;
          }
        }
        it.el.style.setProperty(tr.name, v.toFixed(2));
      });
    } else if (it.kind === "path") {
      if (!it.pts) it.pts = it.keys.map(function (k) { return { t: k.t, xy: point(k.to) }; });
      var ks = it.pts, xy = ks[0].xy;
      if (t >= ks[ks.length - 1].t) xy = ks[ks.length - 1].xy;
      else for (var j = 0; j < ks.length - 1; j++) {
        if (t >= ks[j].t && t < ks[j + 1].t) {
          var e = ease.io((t - ks[j].t) / (ks[j + 1].t - ks[j].t));
          xy = [ks[j].xy[0] + (ks[j + 1].xy[0] - ks[j].xy[0]) * e, ks[j].xy[1] + (ks[j + 1].xy[1] - ks[j].xy[1]) * e];
          break;
        }
      }
      it.el.style.setProperty("--x", xy[0].toFixed(1));
      it.el.style.setProperty("--y", xy[1].toFixed(1));
    } else if (it.kind === "type") {
      var p = clamp((t - it.at) / it.dur);
      var n = Math.round(p * it.text.length);
      if (n !== it.shown) { it.el.textContent = it.text.slice(0, n); it.shown = n; }
      it.el.classList.toggle("armed", t >= it.at - 0.3 && t < it.at);
      it.el.classList.toggle("typing", t >= it.at && p < 1);
      it.el.classList.toggle("typed", p >= 1);
    } else if (it.kind === "scramble") {
      var q = clamp((t - it.at) / it.dur);
      var out;
      if (q <= 0) out = it.from;
      else if (q >= 1) out = it.text;
      else {
        var len = Math.round(it.from.length + (it.text.length - it.from.length) * ease.out(q));
        var frame = Math.floor(t * 30);
        out = "";
        for (var i = 0; i < len; i++) {
          // Characters settle left to right.
          var settle = 0.25 + 0.75 * (i / Math.max(1, it.text.length));
          if (q >= settle && i < it.text.length) out += it.text[i];
          else out += GLYPHS[Math.floor(hash(i + 1, frame) * GLYPHS.length)];
        }
      }
      if (out !== it.shown) { it.el.textContent = out; it.shown = out; }
    }
  }

  function render(t) {
    stage.style.setProperty("--t", t.toFixed(3));
    globals.forEach(function (it) { renderItem(it, t); });
    scenes.forEach(function (sc) {
      var end = sc.tout + FADE;
      var on = t >= sc.tin - 0.001 && t < end;
      sc.el.style.visibility = on ? "visible" : "hidden";
      if (!on) return;
      var pin = sc.tin <= 0 ? 1 : clamp((t - sc.tin) / FADE);
      var pout = clamp((t - sc.tout) / FADE);
      sc.el.style.setProperty("--in", ease.io(pin).toFixed(4));
      sc.el.style.setProperty("--out", ease.io(pout).toFixed(4));
      sc.el.style.setProperty("--sp", clamp((t - sc.tin) / (end - sc.tin)).toFixed(4));
      var sx = 0, sy = 0;
      sc.hits.forEach(function (h, i) {
        var d = t - h;
        if (d < 0 || d > 0.6) return;
        var a = Math.exp(-d * 9);
        sx += a * Math.sin(d * 70 + i) ;
        sy += a * Math.cos(d * 55 + i * 2);
      });
      sc.el.style.setProperty("--shx", sx.toFixed(3));
      sc.el.style.setProperty("--shy", sy.toFixed(3));
      sc.items.forEach(function (it) { renderItem(it, t); });
    });
  }

  // ------------------------------------------------------------ sound cues
  // Key clicks follow every typed text marked .keys; the rest is declared
  // with data-sfx. The exporter turns this list into a sound track.
  function sfx() {
    var list = [];
    all("[data-sfx]").forEach(function (el) {
      el.getAttribute("data-sfx").trim().split(/\s+/).forEach(function (part) {
        var m = /^(\w+)@(.+)$/.exec(part);
        if (!m) throw new Error("bad data-sfx: " + part);
        list.push({ kind: m[1], t: +time(m[2]).toFixed(3) });
      });
    });
    all("[data-type].keys").forEach(function (el) {
      var sp = span(el.getAttribute("data-type"));
      var at = sp.at, dur = sp.dur, n = (el.getAttribute("data-text") || "").length;
      var step = Math.max(0.055, dur / Math.max(1, n));
      for (var x = at; x < at + dur - 0.01; x += step) {
        list.push({ kind: "key", t: +(x + (hash(Math.round(x * 1000), 7) - 0.5) * 0.02).toFixed(3) });
      }
    });
    return list.sort(function (a, b) { return a.t - b.t; });
  }

  // --------------------------------------------------------------- playback
  function fit() {
    var s = Math.min(window.innerWidth / 1920, window.innerHeight / 1080);
    stage.style.setProperty("--fit", s.toFixed(4));
  }
  window.addEventListener("resize", fit);
  fit();

  var t = 0, playing = false, last = null;
  var audio = null;
  function frame(now) {
    if (!playing) return;
    if (audio) t = audio.currentTime;
    else if (last !== null) t += (now - last) / 1000;
    last = now;
    if (t >= duration) { t = duration - 0.001; playing = false; }
    render(t);
    if (playing) requestAnimationFrame(frame);
  }
  function play() {
    if (playing) return;
    playing = true; last = null;
    if (audio) { audio.currentTime = t; audio.play(); }
    requestAnimationFrame(frame);
  }
  function pause() { playing = false; if (audio) audio.pause(); }
  function seek(x) { t = Math.max(0, Math.min(duration - 0.001, x)); render(t); }

  // Preview: ?audio=<url> plays along; space toggles; ?t=12.3 opens on a frame.
  if (params.get("audio")) {
    audio = new Audio(params.get("audio"));
    audio.preload = "auto";
  }
  document.addEventListener("keydown", function (e) {
    if (e.code === "Space") { e.preventDefault(); playing ? pause() : play(); }
    if (e.code === "ArrowRight") seek(t + 1);
    if (e.code === "ArrowLeft") seek(t - 1);
  });
  // Paths measure the layout, so they wait for the fonts.
  var ready = document.fonts.ready.then(function () {
    scenes.concat([{ items: globals }]).forEach(function (sc) {
      sc.items.forEach(function (it) { if (it.kind === "path") it.pts = null; });
    });
    render(t);
  });
  seek(params.has("t") ? parseFloat(params.get("t")) : 0);

  window.Film = { lang: lang, duration: duration, cues: cues, ready: ready, seek: seek, play: play, pause: pause, sfx: sfx };
})();
