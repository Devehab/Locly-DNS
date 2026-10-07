// Locly landing page: language switch, install tabs and copy buttons.
(function () {
  "use strict";

  var root = document.documentElement;
  var titles = {
    en: "Locly — Friendly local hostnames for developers and AI agents",
    ar: "Locly — أسماء سهلة لخدماتك المحلية، للمطورين ووكلاء الذكاء الاصطناعي"
  };

  // ---------------------------------------------------------------- language
  function setLang(lang, persist) {
    root.lang = lang;
    root.dir = lang === "ar" ? "rtl" : "ltr";
    document.title = titles[lang];
    if (persist) {
      try {
        localStorage.setItem("locly-lang", lang);
        var url = new URL(location.href);
        url.searchParams.set("lang", lang);
        history.replaceState(null, "", url);
      } catch (e) {}
    }
  }

  setLang(root.lang === "ar" ? "ar" : "en", false);

  var toggle = document.getElementById("lang-toggle");
  if (toggle) {
    toggle.addEventListener("click", function () {
      setLang(root.lang === "ar" ? "en" : "ar", true);
    });
  }

  // -------------------------------------------------------------------- tabs
  var groups = Array.prototype.slice.call(document.querySelectorAll("[data-tabs]"));

  function selectOS(os, focusGroup) {
    groups.forEach(function (group) {
      var tabs = group.querySelectorAll('[role="tab"]');
      Array.prototype.forEach.call(tabs, function (tab) {
        var on = tab.getAttribute("data-os") === os;
        tab.setAttribute("aria-selected", on ? "true" : "false");
        tab.tabIndex = on ? 0 : -1;
        var panel = document.getElementById(tab.getAttribute("aria-controls"));
        if (panel) panel.hidden = !on;
        if (on && group === focusGroup) tab.focus();
      });
    });
  }

  groups.forEach(function (group) {
    var tabs = Array.prototype.slice.call(group.querySelectorAll('[role="tab"]'));
    tabs.forEach(function (tab, i) {
      tab.addEventListener("click", function () {
        selectOS(tab.getAttribute("data-os"));
      });
      tab.addEventListener("keydown", function (ev) {
        var keys = { ArrowRight: 1, ArrowLeft: -1, Home: "first", End: "last" };
        var k = keys[ev.key];
        if (k === undefined) return;
        ev.preventDefault();
        var step = typeof k === "number" ? (root.dir === "rtl" ? -k : k) : 0;
        var next = k === "first" ? 0 : k === "last" ? tabs.length - 1 : (i + step + tabs.length) % tabs.length;
        selectOS(tabs[next].getAttribute("data-os"), group);
      });
    });
  });

  // Show the Windows command first to Windows visitors.
  var platform = (navigator.userAgentData && navigator.userAgentData.platform) || navigator.platform || navigator.userAgent || "";
  if (/win/i.test(platform)) selectOS("windows");

  // ------------------------------------------- fade commands that scroll
  var scrollers = Array.prototype.slice.call(document.querySelectorAll(".cmd > code"));
  function updateFade(el) {
    var more = el.scrollWidth - el.clientWidth - el.scrollLeft > 2;
    el.classList.toggle("overflowing", more);
  }
  scrollers.forEach(function (el) {
    el.addEventListener("scroll", function () { updateFade(el); }, { passive: true });
  });
  function updateAll() { scrollers.forEach(updateFade); }
  window.addEventListener("resize", updateAll);
  window.addEventListener("load", updateAll);
  document.addEventListener("click", function () { setTimeout(updateAll, 0); });
  updateAll();

  // -------------------------------------------------------------------- copy
  function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) {
      return navigator.clipboard.writeText(text);
    }
    return new Promise(function (resolve, reject) {
      var ta = document.createElement("textarea");
      ta.value = text;
      ta.setAttribute("readonly", "");
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      try {
        document.execCommand("copy") ? resolve() : reject(new Error("copy failed"));
      } catch (e) {
        reject(e);
      } finally {
        document.body.removeChild(ta);
      }
    });
  }

  var labels = {
    copy: { en: "Copy", ar: "نسخ" },
    done: { en: "Copied!", ar: "تم النسخ!" }
  };

  function setLabel(btn, key) {
    var en = btn.querySelector(".copy-label .en");
    var ar = btn.querySelector(".copy-label .ar");
    if (en) en.textContent = labels[key].en;
    if (ar) ar.textContent = labels[key].ar;
    var use = btn.querySelector("use");
    if (use) use.setAttribute("href", key === "done" ? "#i-check" : "#i-copy");
  }

  document.querySelectorAll(".copy[data-copy]").forEach(function (btn) {
    var timer = null;
    btn.setAttribute("title", "Copy · نسخ");
    btn.addEventListener("click", function () {
      copyText(btn.getAttribute("data-copy")).then(function () {
        btn.classList.add("copied");
        setLabel(btn, "done");
        clearTimeout(timer);
        timer = setTimeout(function () {
          btn.classList.remove("copied");
          setLabel(btn, "copy");
        }, 1800);
      });
    });
  });
})();
