// LocalDNS terminal guide: language switch and copy buttons. The same page is
// served by the dashboard (/guide) and published on the website.
(function () {
  "use strict";

  var root = document.documentElement;
  var toggle = document.getElementById("lang-toggle");

  function store(key, value) {
    try {
      if (value === undefined) return localStorage.getItem(key);
      localStorage.setItem(key, value);
    } catch (e) {}
    return null;
  }

  function setLang(lang) {
    root.lang = lang;
    root.dir = lang === "ar" ? "rtl" : "ltr";
    toggle.textContent = lang === "ar" ? "English" : "العربية";
    toggle.lang = lang === "ar" ? "en" : "ar";
    store("localdns-lang", lang);
  }

  // ?lang=ar, then the choice made here or on the Locly website, then the
  // browser's language.
  var param = new URLSearchParams(location.search).get("lang");
  var saved = param || store("localdns-lang") || store("locly-lang");
  var browserArabic = /^ar\b/i.test(navigator.language || "");
  setLang(saved === "ar" || saved === "en" ? saved : (browserArabic ? "ar" : "en"));
  toggle.addEventListener("click", function () { setLang(root.lang === "ar" ? "en" : "ar"); });

  // In the dashboard (which serves this page at exactly /guide) "back"
  // returns to it; on the website (…/guide/) it goes to the home page.
  if (location.pathname !== "/guide") {
    var back = document.getElementById("back-link");
    back.href = "../";
    back.querySelector(".en").textContent = "← Locly website";
    back.querySelector(".ar").textContent = "→ موقع Locly";
  }

  // Copy buttons. navigator.clipboard needs a secure page, which
  // http://localdns.local is not, so fall back to a hidden text area.
  function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) {
      return navigator.clipboard.writeText(text);
    }
    return new Promise(function (resolve, reject) {
      var area = document.createElement("textarea");
      area.value = text;
      area.setAttribute("readonly", "");
      area.className = "copy-area";
      document.body.appendChild(area);
      area.select();
      var ok = false;
      try { ok = document.execCommand("copy"); } catch (e) {}
      document.body.removeChild(area);
      if (ok) { resolve(); } else { reject(new Error("copy failed")); }
    });
  }

  function label(en, ar) {
    var frag = document.createDocumentFragment();
    var e = document.createElement("span");
    e.className = "en";
    e.textContent = en;
    var a = document.createElement("span");
    a.className = "ar";
    a.textContent = ar;
    frag.appendChild(e);
    frag.appendChild(a);
    return frag;
  }

  // Each command sits in a row with its copy button beside it (never on top
  // of a long command).
  document.querySelectorAll("pre.cmd").forEach(function (pre) {
    var code = pre.querySelector("code");
    var row = document.createElement("div");
    row.className = "cmd-row";
    pre.parentNode.insertBefore(row, pre);
    row.appendChild(pre);
    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = "copy-btn";
    btn.setAttribute("aria-label", "Copy command");
    btn.appendChild(label("Copy", "نسخ"));
    btn.addEventListener("click", function () {
      copyText(code.textContent).then(function () {
        btn.textContent = "";
        btn.appendChild(label("Copied ✓", "تم النسخ ✓"));
        btn.classList.add("copied");
        setTimeout(function () {
          btn.textContent = "";
          btn.appendChild(label("Copy", "نسخ"));
          btn.classList.remove("copied");
        }, 1600);
      }).catch(function () {});
    });
    row.appendChild(btn);
  });
})();
