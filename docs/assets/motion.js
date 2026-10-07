// Locly story animation.
//
// One timeline drives everything. Each animated element declares when it
// moves with data-v="name@start/duration[:easing] …" (times in seconds,
// relative to its scene). The engine turns that into an eased 0→1 CSS
// variable (--name) and CSS does the rest. Because a frame depends only on
// the time t, any moment can be rendered exactly: LoclyMotion.seek(t).
(function () {
  "use strict";

  var stage = document.getElementById("motion-stage");
  if (!stage) return;

  var DURATION = 34.5;
  var FADE = 0.45;
  var CHAPTERS = [
    { start: 0, end: 5.1, poster: 3.6 },
    { start: 5.1, end: 10.5, poster: 8.8 },
    { start: 10.5, end: 15.6, poster: 14.6 },
    { start: 15.6, end: 22.2, poster: 21.6 },
    { start: 22.2, end: 30, poster: 29.4 },
    { start: 30, end: DURATION, poster: 33 }
  ];

  var ease = {
    out: function (p) { return 1 - Math.pow(1 - p, 3); },
    io: function (p) { return p < 0.5 ? 4 * p * p * p : 1 - Math.pow(-2 * p + 2, 3) / 2; },
    back: function (p) { var c = 1.70158; return 1 + (c + 1) * Math.pow(p - 1, 3) + c * Math.pow(p - 1, 2); },
    lin: function (p) { return p; }
  };

  function clamp(v) { return v < 0 ? 0 : v > 1 ? 1 : v; }

  function parseSpecs(str) {
    return str.trim().split(/\s+/).map(function (part) {
      var m = /^(\w+)@([\d.]+)\/([\d.]+)(?::(\w+))?$/.exec(part);
      if (!m) throw new Error("bad data-v: " + part);
      return { name: "--" + m[1], at: +m[2], dur: +m[3], ease: ease[m[4] || "out"] || ease.out };
    });
  }

  var scenes = Array.prototype.map.call(stage.querySelectorAll(".m-scene"), function (el) {
    return {
      el: el,
      start: +el.getAttribute("data-start"),
      end: +el.getAttribute("data-end"),
      items: Array.prototype.map.call(el.querySelectorAll("[data-v]"), function (node) {
        return { el: node, specs: parseSpecs(node.getAttribute("data-v")) };
      }),
      typers: Array.prototype.map.call(el.querySelectorAll("[data-type]"), function (node) {
        var parts = node.getAttribute("data-type").split("/");
        return { el: node, at: +parts[0], dur: +parts[1], text: node.getAttribute("data-text") || "", shown: -1 };
      })
    };
  });

  var chapterButtons = Array.prototype.slice.call(document.querySelectorAll(".motion-chapters [data-chapter]"));
  var playButton = document.getElementById("motion-play");

  function render(t) {
    stage.style.setProperty("--t", t.toFixed(3));
    scenes.forEach(function (sc) {
      var local = t - sc.start;
      var active = t >= sc.start && t < sc.end;
      var vis = 0;
      if (active) {
        var fadeIn = sc.start === 0 && t < FADE ? 1 : local / FADE;
        vis = clamp(Math.min(fadeIn, (sc.end - t) / FADE));
      }
      sc.el.style.setProperty("--vis", ease.io(vis).toFixed(4));
      sc.el.style.visibility = vis > 0 ? "visible" : "hidden";
      if (!active) return;
      sc.items.forEach(function (item) {
        item.specs.forEach(function (sp) {
          var p = sp.dur > 0 ? clamp((local - sp.at) / sp.dur) : (local >= sp.at ? 1 : 0);
          item.el.style.setProperty(sp.name, sp.ease(p).toFixed(4));
        });
      });
      sc.typers.forEach(function (ty) {
        var p = clamp((local - ty.at) / ty.dur);
        var n = Math.round(p * ty.text.length);
        if (n !== ty.shown) {
          ty.el.textContent = ty.text.slice(0, n);
          ty.shown = n;
        }
        ty.el.classList.toggle("is-typing", local >= ty.at - 0.4 && p < 1);
      });
    });
    chapterButtons.forEach(function (btn, i) {
      var c = CHAPTERS[i];
      var fill = clamp((t - c.start) / (c.end - c.start));
      btn.style.setProperty("--fill", fill.toFixed(4));
      btn.classList.toggle("is-current", t >= c.start && t < c.end);
    });
  }

  // ------------------------------------------------------------- playback
  var t = 0;
  var playing = false;
  var userPaused = false;
  var lastFrame = null;
  var reduceMotion = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  function frame(now) {
    if (!playing) return;
    if (lastFrame !== null) {
      t += Math.min(0.1, (now - lastFrame) / 1000);
      if (t >= DURATION) t -= DURATION;
    }
    lastFrame = now;
    render(t);
    requestAnimationFrame(frame);
  }

  function setPlaying(on) {
    if (on === playing) return;
    playing = on;
    lastFrame = null;
    stage.classList.toggle("is-playing", on);
    if (playButton) {
      playButton.setAttribute("aria-pressed", on ? "true" : "false");
      playButton.classList.toggle("is-playing", on);
    }
    if (on) requestAnimationFrame(frame);
  }

  function seek(time) {
    t = Math.max(0, Math.min(DURATION - 0.001, time));
    render(t);
  }

  if (playButton) {
    playButton.addEventListener("click", function () {
      userPaused = playing;
      setPlaying(!playing);
    });
  }

  chapterButtons.forEach(function (btn, i) {
    btn.addEventListener("click", function () {
      var c = CHAPTERS[i];
      if (playing) {
        seek(c.start);
      } else {
        seek(c.poster);
      }
    });
  });

  // Play while the story is on screen (unless the visitor paused it or
  // prefers reduced motion); pause when it scrolls away or the tab hides.
  var inView = false;
  function autoplay() {
    setPlaying(inView && !userPaused && !reduceMotion && !document.hidden);
  }
  if ("IntersectionObserver" in window) {
    new IntersectionObserver(function (entries) {
      inView = entries[0].isIntersecting;
      autoplay();
    }, { threshold: 0.35 }).observe(stage);
  }
  document.addEventListener("visibilitychange", autoplay);

  // Start on a meaningful frame (the problem, fully shown).
  seek(reduceMotion ? CHAPTERS[0].poster : 0);

  window.LoclyMotion = {
    duration: DURATION,
    chapters: CHAPTERS,
    seek: function (time) { setPlaying(false); userPaused = true; seek(time); },
    play: function () { userPaused = false; setPlaying(true); },
    pause: function () { userPaused = true; setPlaying(false); }
  };
})();
