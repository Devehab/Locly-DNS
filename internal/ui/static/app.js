// LocalDNS web UI. Talks only to the local LocalDNS server that served it.
(function () {
  "use strict";

  var token = document.querySelector('meta[name="localdns-token"]').content;
  var $ = function (id) { return document.getElementById(id); };

  var rows = $("rows");
  var banner = $("banner");
  var addDialog = $("add-dialog");
  var editDialog = $("edit-dialog");
  var deleteDialog = $("delete-dialog");
  var routerOffDialog = $("router-off-dialog");
  var pendingDelete = null;
  var pendingEdit = null;
  var readOnlyHint = "";
  var routerState = null;
  var lastEntries = [];

  function api(method, path, body) {
    var opts = { method: method, headers: { "X-LocalDNS-Token": token }, cache: "no-store" };
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(body);
    }
    return fetch(path, opts).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        if (res.status === 403 && data.error && /token/i.test(data.error.message || "")) {
          // The UI was restarted (each run has a new security token) and this
          // tab is from the previous run: reload once to pick up the new one.
          var last = 0;
          try { last = +sessionStorage.getItem("localdns-reloaded") || 0; } catch (e) {}
          if (Date.now() - last > 10000) {
            try { sessionStorage.setItem("localdns-reloaded", String(Date.now())); } catch (e) {}
            showBanner("LocalDNS was restarted. Reloading…", "warn");
            location.reload();
            return new Promise(function () {});
          }
          throw { message: "This page belongs to a LocalDNS UI that is no longer running", hint: "Run `localdns ui` and open the address it prints." };
        }
        if (!res.ok) {
          var err = (data && data.error) || { message: "Request failed (" + res.status + ")" };
          throw err;
        }
        return data;
      });
    });
  }

  function errorText(err) {
    if (!err) return "Something went wrong.";
    return err.hint ? err.message + ". " + err.hint : err.message || String(err);
  }

  function showBanner(text, kind) {
    banner.textContent = text;
    banner.className = "banner" + (kind ? " banner-" + kind : "");
    banner.hidden = !text;
  }

  var toastTimer = null;
  function toast(text) {
    var el = $("toast");
    el.textContent = text;
    el.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { el.hidden = true; }, 3500);
  }

  function cell(text, className) {
    var td = document.createElement("td");
    if (className) td.className = className;
    if (text !== undefined && text !== null) td.textContent = text;
    return td;
  }

  function button(text, className, label, onClick) {
    var b = document.createElement("button");
    b.type = "button";
    b.className = className;
    b.textContent = text;
    b.setAttribute("aria-label", label);
    b.addEventListener("click", onClick);
    return b;
  }

  // ---- Pasted links -------------------------------------------------------
  // "http://127.0.0.1:3000/" → "127.0.0.1:3000". A path can't be part of a
  // mapping, so it is cut off and shown as a hint instead.

  function cleanValue(value) {
    var v = value.trim();
    var scheme = v.match(/^[a-z][a-z0-9+.-]*:\/\//i);
    if (scheme && /^https?:\/\/$/i.test(scheme[0])) v = v.slice(scheme[0].length);
    var path = "";
    var cut = v.search(/[\/?#]/);
    if (scheme && /^https?:\/\/$/i.test(scheme[0]) && cut >= 0) {
      path = v.slice(cut).replace(/^\/+$/, "");
      v = v.slice(0, cut);
    } else {
      v = v.replace(/\/+$/, "");
    }
    return { value: v, path: path, changed: v !== value };
  }

  function setNote(el, text) {
    el.textContent = text || "";
    el.hidden = !text;
  }

  function attachCleaner(input, note, hostInput) {
    function clean() {
      var c = cleanValue(input.value);
      if (!c.changed) return;
      input.value = c.value;
      if (c.path) {
        var host = (hostInput && hostInput.value.trim()) || "app.local";
        setNote(note, "Only the address is kept. Open the page as http://" + host + c.path);
      } else if (c.value) {
        setNote(note, "✓ Link cleaned up: " + c.value);
      }
    }
    input.addEventListener("paste", function () { setTimeout(clean, 0); });
    input.addEventListener("blur", clean);
    return clean;
  }

  // ---- Endings (.local, .test, …) -----------------------------------------
  // One click adds the ending to the name typed so far, or swaps the ending
  // it already has.

  // .localhost first: browsers treat it as secure, like localhost itself.
  var preferredOrder = ["localhost", "local", "test", "internal", "lan", "home.arpa", "example", "localdomain"];
  var suffixes = preferredOrder.slice();

  function splitName(value) {
    var v = value.trim().toLowerCase().replace(/\.+$/, "");
    var longestFirst = suffixes.slice().sort(function (a, b) { return b.length - a.length; });
    for (var i = 0; i < longestFirst.length; i++) {
      var s = longestFirst[i];
      if (v === s) return { base: "", suffix: s };
      if (v.slice(-(s.length + 1)) === "." + s) return { base: v.slice(0, -(s.length + 1)), suffix: s };
    }
    return { base: v, suffix: "" };
  }

  function renderSuffixes(box, input) {
    box.querySelectorAll("button").forEach(function (b) { box.removeChild(b); });
    suffixes.forEach(function (s) {
      var b = document.createElement("button");
      b.type = "button";
      b.className = "chip";
      b.textContent = "." + s;
      b.dataset.suffix = s;
      b.addEventListener("click", function () {
        var parts = splitName(cleanValue(input.value).value);
        input.focus();
        if (parts.base) {
          input.value = parts.base + "." + s;
          input.setSelectionRange(input.value.length, input.value.length);
        } else {
          // Nothing typed yet: put the ending in and the cursor before it.
          input.value = "." + s;
          input.setSelectionRange(0, 0);
        }
        markSuffix(box, input);
        input.dispatchEvent(new Event("input"));
      });
      box.appendChild(b);
    });
    markSuffix(box, input);
  }

  function markSuffix(box, input) {
    var current = splitName(input.value).suffix;
    box.querySelectorAll("button").forEach(function (b) {
      var on = b.dataset.suffix === current;
      b.classList.toggle("chip-on", on);
      b.setAttribute("aria-pressed", on ? "true" : "false");
    });
  }

  function setSuffixes(list) {
    if (!list || !list.length) return;
    var ordered = preferredOrder.filter(function (s) { return list.indexOf(s) >= 0; });
    list.forEach(function (s) { if (ordered.indexOf(s) < 0) ordered.push(s); });
    if (ordered.join() === suffixes.join()) return;
    suffixes = ordered;
    renderSuffixes($("add-suffixes"), $("add-hostname"));
    renderSuffixes($("edit-suffixes"), $("edit-hostname"));
  }

  // The hint under the endings explains the choice. Browsers enable some
  // features (crypto.subtle, the clipboard, many sign-in flows) only on
  // https:// or localhost pages, and treat *.localhost like localhost; a
  // .localhost name can only ever mean this computer.
  function isThisComputer(address) {
    var a = cleanValue(address).value.toLowerCase();
    return a === "" || /^localhost(:\d+)?$/.test(a) || /^127\./.test(a) || /^(\[::1\](:\d+)?|::1)$/.test(a);
  }

  function updateHint(hint, hostInput, addrInput) {
    var suffix = splitName(hostInput.value).suffix;
    var here = isThisComputer(addrInput.value);
    var text = "";
    var warn = false;
    if (suffix === "localhost" && here) {
      text = "✓ Browsers treat .localhost as secure, like localhost: sign-in, uploads and copy work as they do there.";
    } else if (suffix === "localhost") {
      text = ".localhost always means this computer. For another device, pick .local or .lan.";
      warn = true;
    } else if (here) {
      text = "Tip: for apps on this computer, .localhost works best. Browsers treat it as secure, like localhost; " +
        "with other endings some features (sign-in, uploads) may not work.";
    }
    hint.textContent = text;
    hint.hidden = !text;
    hint.classList.toggle("suffix-hint-warn", warn);
  }

  var hintUpdaters = {};
  [["add-suffixes", "add-hostname", "add-address", "add-suffix-hint"],
   ["edit-suffixes", "edit-hostname", "edit-address", "edit-suffix-hint"]].forEach(function (ids) {
    var box = $(ids[0]);
    var input = $(ids[1]);
    var address = $(ids[2]);
    var hint = $(ids[3]);
    var update = function () { updateHint(hint, input, address); };
    hintUpdaters[ids[0]] = update;
    renderSuffixes(box, input);
    input.addEventListener("input", function () { markSuffix(box, input); update(); });
    address.addEventListener("input", update);
    address.addEventListener("blur", function () { setTimeout(update, 0); });
    address.addEventListener("paste", function () { setTimeout(update, 10); });
  });

  var cleanAddHost = attachCleaner($("add-hostname"), $("add-note"));
  var cleanAddAddress = attachCleaner($("add-address"), $("add-note"), $("add-hostname"));
  var cleanEditHost = attachCleaner($("edit-hostname"), $("edit-note"));
  var cleanEditAddress = attachCleaner($("edit-address"), $("edit-note"), $("edit-hostname"));

  // ---- Table --------------------------------------------------------------

  var statusLabels = { active: "Active", missing: "Missing", conflict: "Conflict", paused: "Paused" };

  function render(entries) {
    lastEntries = entries;
    $("loading").hidden = true;
    rows.textContent = "";
    $("empty").hidden = entries.length > 0;
    entries.forEach(function (e) {
      var paused = e.status === "paused";
      var tr = document.createElement("tr");
      if (paused) tr.className = "row-paused";
      tr.appendChild(cell(e.hostname, "mono strong fade"));
      tr.appendChild(cell(e.ip, "mono fade"));
      tr.appendChild(cell(e.port === null ? "—" : String(e.port), "mono fade"));

      var urlTd = cell(null, "mono fade");
      // Prefer the port-free address when the LocalDNS router serves it.
      var open = e.short_url || e.url;
      if (paused) {
        urlTd.textContent = e.url;
        urlTd.title = "Paused: switch it on to use this address";
      } else if (open) {
        var a = document.createElement("a");
        a.href = open;
        a.textContent = open;
        a.target = "_blank";
        a.rel = "noopener noreferrer";
        if (e.short_url) a.title = "Same as " + e.url;
        urlTd.appendChild(a);
      } else {
        urlTd.textContent = "—";
      }
      tr.appendChild(urlTd);

      var statusTd = cell(null);
      var statusWrap = document.createElement("div");
      statusWrap.className = "status-wrap";
      statusWrap.appendChild(entrySwitch(e));
      statusTd.appendChild(statusWrap);
      var badge = document.createElement("span");
      badge.className = "status status-" + e.status;
      var dot = document.createElement("span");
      dot.className = "dot";
      dot.setAttribute("aria-hidden", "true");
      badge.appendChild(dot);
      badge.appendChild(document.createTextNode(statusLabels[e.status] || e.status));
      if (e.detail) badge.title = e.detail;
      statusWrap.appendChild(badge);
      tr.appendChild(statusTd);

      var actions = cell(null, "actions-cell");
      actions.appendChild(button("Edit", "btn btn-small btn-ghost", "Edit " + e.hostname, function () { openEdit(e); }));
      actions.appendChild(button("Delete", "btn btn-small btn-ghost-danger", "Delete " + e.hostname, function () { openDelete(e); }));
      tr.appendChild(actions);

      rows.appendChild(tr);
    });
    renderRouter();
  }

  // An on/off switch per name: off pauses it (kept, but out of the hosts
  // file), on resumes it.
  function entrySwitch(e) {
    var label = document.createElement("label");
    label.className = "switch switch-small";
    label.title = e.status === "paused" ? "Paused: switch on to use " + e.hostname : "On: switch off to pause " + e.hostname;
    var input = document.createElement("input");
    input.type = "checkbox";
    input.setAttribute("role", "switch");
    input.setAttribute("aria-label", e.hostname + " on");
    input.checked = e.status !== "paused";
    input.disabled = !!readOnlyHint;
    input.addEventListener("change", function () {
      input.disabled = true;
      var action = input.checked ? "resume" : "pause";
      api("POST", "/api/entries/" + encodeURIComponent(e.hostname) + "/" + action, {})
        .then(function () {
          toast(action === "pause"
            ? "✓ " + e.hostname + " paused. It stays here; switch it on any time."
            : "✓ " + e.hostname + " is on again");
          return refresh();
        })
        .catch(function (err) {
          input.checked = !input.checked;
          input.disabled = false;
          showBanner(errorText(err), "error");
        });
    });
    var slider = document.createElement("span");
    slider.className = "slider";
    slider.setAttribute("aria-hidden", "true");
    label.appendChild(input);
    label.appendChild(slider);
    return label;
  }

  // ---- Port-free URLs (router) --------------------------------------------

  function exampleEntry() {
    for (var i = 0; i < lastEntries.length; i++) {
      var e = lastEntries[i];
      if (e.port !== null && e.ip === "127.0.0.1" && e.status === "active" && e.hostname !== "localdns.local") return e;
    }
    return null;
  }

  function renderRouter() {
    var card = $("router-card");
    var r = routerState;
    if (!r || !r.supported) { card.hidden = true; return; }
    card.hidden = false;
    var on = r.running;
    var e = exampleEntry();
    var name = e ? e.hostname : "app.local";
    var port = e ? e.port : 3000;
    var toggle = $("router-toggle");
    toggle.checked = on;
    toggle.disabled = !r.can_change;
    var pill = $("router-state");
    pill.textContent = on ? "On" : "Off";
    pill.className = "pill " + (on ? "pill-on" : "pill-off");
    $("router-desc").textContent = on
      ? "Open http://" + name + " instead of http://" + name + ":" + port + "."
      : "Names need their port, e.g. http://" + name + ":" + port + ". Turn this on to open http://" + name + ".";
    var direct = "The dashboard always works at " + r.direct_url + ".";
    if (on && r.dashboard_url) direct = "The dashboard is at " + r.dashboard_url + ", and always at " + r.direct_url + ".";
    if (!r.can_change) direct += " To change this switch, restart the dashboard with: sudo localdns ui";
    if (r.last_error) direct += " " + r.last_error;
    $("router-direct").textContent = direct;
    $("router-off-example").textContent = "http://" + name + ":" + port;
    $("router-off-direct").textContent = r.direct_url;
  }

  function loadRouter() {
    return api("GET", "/api/router").then(function (r) { routerState = r; renderRouter(); })
      .catch(function () { routerState = null; renderRouter(); });
  }

  $("router-toggle").addEventListener("change", function (ev) {
    var toggle = ev.target;
    if (!toggle.checked) {
      // Turning off: explain first that the dashboard then needs its IP.
      toggle.checked = true;
      setError($("router-off-error"), "");
      routerOffDialog.showModal();
      return;
    }
    toggle.disabled = true;
    toast("Turning on port-free URLs…");
    api("POST", "/api/router", { enabled: true })
      .then(function (r) {
        routerState = r;
        toast(r.running ? "✓ Port-free URLs are on" : "Port-free URLs are starting…");
        return refresh();
      })
      .catch(function (err) { showBanner(errorText(err), "error"); return loadRouter(); });
  });

  $("router-off-form").addEventListener("submit", function (ev) {
    ev.preventDefault();
    var submit = $("router-off-submit");
    submit.disabled = true;
    api("POST", "/api/router", { enabled: false })
      .then(function (r) {
        routerOffDialog.close();
        routerState = r;
        renderRouter();
        var viaName = location.hostname !== "127.0.0.1" && location.hostname !== "localhost";
        if (viaName) {
          // This page came through the router, which is stopping now.
          showBanner("Port-free URLs are off. Moving the dashboard to " + r.direct_url + " …", "warn");
          setTimeout(function () { location.href = r.direct_url + "/"; }, 1500);
        } else {
          toast("✓ Port-free URLs are off");
          setTimeout(refresh, 1500);
        }
      })
      .catch(function (err) { setError($("router-off-error"), errorText(err)); })
      .then(function () { submit.disabled = false; });
  });

  // ---- Refresh ------------------------------------------------------------

  function refresh() {
    return Promise.all([api("GET", "/api/status"), api("GET", "/api/entries"), loadRouter()])
      .then(function (results) {
        var status = results[0];
        setSuffixes(status.local_suffixes);
        readOnlyHint = status.writable ? "" : (status.read_only_hint || "The hosts file is read-only.");
        $("footer-hosts").textContent = "Hosts file: " + status.hosts_file;
        if (readOnlyHint) {
          showBanner("Read-only: you can view hosts but not add, edit or delete them. " + readOnlyHint, "warn");
        } else {
          showBanner("");
        }
        render(results[1].entries);
      })
      .catch(function (err) {
        $("loading").hidden = true;
        showBanner(errorText(err), "error");
      });
  }

  function setError(el, text) {
    el.textContent = text || "";
    el.hidden = !text;
  }

  // ---- Add ----------------------------------------------------------------

  $("add-button").addEventListener("click", function () {
    $("add-form").reset();
    markSuffix($("add-suffixes"), $("add-hostname"));
    hintUpdaters["add-suffixes"]();
    setNote($("add-note"), "");
    setError($("add-error"), readOnlyHint ? "Read-only: " + readOnlyHint : "");
    addDialog.showModal();
    $("add-hostname").focus();
  });

  $("add-form").addEventListener("submit", function (ev) {
    ev.preventDefault();
    cleanAddHost();
    cleanAddAddress();
    var hostname = $("add-hostname").value.trim();
    var address = $("add-address").value.trim();
    if (!hostname || !address) {
      setError($("add-error"), "Enter a hostname and an address.");
      return;
    }
    var submit = $("add-submit");
    submit.disabled = true;
    api("POST", "/api/entries", { hostname: hostname, address: address })
      .then(function (res) {
        addDialog.close();
        toast(res.action === "unchanged" ? "✓ Host already exists" : "✓ Host added successfully");
        return refresh();
      })
      .catch(function (err) { setError($("add-error"), errorText(err)); })
      .then(function () { submit.disabled = false; });
  });

  // ---- Edit ---------------------------------------------------------------

  function openEdit(entry) {
    pendingEdit = entry;
    $("edit-name").textContent = entry.hostname;
    $("edit-hostname").value = entry.hostname;
    $("edit-address").value = entry.address;
    markSuffix($("edit-suffixes"), $("edit-hostname"));
    hintUpdaters["edit-suffixes"]();
    setNote($("edit-note"), "");
    setError($("edit-error"), readOnlyHint ? "Read-only: " + readOnlyHint : "");
    editDialog.showModal();
    $("edit-address").focus();
    $("edit-address").select();
  }

  $("edit-form").addEventListener("submit", function (ev) {
    ev.preventDefault();
    if (!pendingEdit) return;
    cleanEditHost();
    cleanEditAddress();
    var hostname = $("edit-hostname").value.trim();
    var address = $("edit-address").value.trim();
    if (!hostname || !address) {
      setError($("edit-error"), "Enter a hostname and an address.");
      return;
    }
    var submit = $("edit-submit");
    submit.disabled = true;
    api("PUT", "/api/entries/" + encodeURIComponent(pendingEdit.hostname), { hostname: hostname, address: address })
      .then(function (res) {
        editDialog.close();
        toast(res.action === "unchanged" ? "Nothing changed" : "✓ " + res.entry.hostname + " updated");
        pendingEdit = null;
        return refresh();
      })
      .catch(function (err) { setError($("edit-error"), errorText(err)); })
      .then(function () { submit.disabled = false; });
  });

  // ---- Delete -------------------------------------------------------------

  function openDelete(entry) {
    pendingDelete = entry;
    $("delete-name").textContent = entry.hostname;
    setError($("delete-error"), "");
    deleteDialog.showModal();
  }

  $("delete-form").addEventListener("submit", function (ev) {
    ev.preventDefault();
    if (!pendingDelete) return;
    var submit = $("delete-submit");
    submit.disabled = true;
    api("DELETE", "/api/entries/" + encodeURIComponent(pendingDelete.hostname))
      .then(function () {
        deleteDialog.close();
        toast("✓ " + pendingDelete.hostname + " deleted");
        pendingDelete = null;
        return refresh();
      })
      .catch(function (err) { setError($("delete-error"), errorText(err)); })
      .then(function () { submit.disabled = false; });
  });

  document.querySelectorAll("[data-close]").forEach(function (btn) {
    btn.addEventListener("click", function () { btn.closest("dialog").close(); });
  });

  // Pick up changes made from the CLI while the page is open.
  document.addEventListener("visibilitychange", function () {
    if (!document.hidden) refresh();
  });
  setInterval(function () { if (!document.hidden) refresh(); }, 10000);

  refresh();
})();
