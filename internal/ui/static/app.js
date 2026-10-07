// LocalDNS web UI. Talks only to the local LocalDNS server that served it.
(function () {
  "use strict";

  var token = document.querySelector('meta[name="localdns-token"]').content;
  var $ = function (id) { return document.getElementById(id); };

  var rows = $("rows");
  var banner = $("banner");
  var addDialog = $("add-dialog");
  var deleteDialog = $("delete-dialog");
  var pendingDelete = null;
  var readOnlyHint = "";

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
    toastTimer = setTimeout(function () { el.hidden = true; }, 3000);
  }

  function cell(text, className) {
    var td = document.createElement("td");
    if (className) td.className = className;
    if (text !== undefined && text !== null) td.textContent = text;
    return td;
  }

  var statusLabels = { active: "Active", missing: "Missing", conflict: "Conflict" };

  function render(entries) {
    $("loading").hidden = true;
    rows.textContent = "";
    $("empty").hidden = entries.length > 0;
    entries.forEach(function (e) {
      var tr = document.createElement("tr");
      tr.appendChild(cell(e.hostname, "mono strong"));
      tr.appendChild(cell(e.ip, "mono"));
      tr.appendChild(cell(e.port === null ? "—" : String(e.port), "mono"));

      var urlTd = cell(null, "mono");
      // Prefer the port-free address when the LocalDNS router serves it.
      var open = e.short_url || e.url;
      if (open) {
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
      var badge = document.createElement("span");
      badge.className = "status status-" + e.status;
      var dot = document.createElement("span");
      dot.className = "dot";
      dot.setAttribute("aria-hidden", "true");
      badge.appendChild(dot);
      badge.appendChild(document.createTextNode(statusLabels[e.status] || e.status));
      if (e.detail) badge.title = e.detail;
      statusTd.appendChild(badge);
      tr.appendChild(statusTd);

      var actions = cell(null, "actions-cell");
      var del = document.createElement("button");
      del.type = "button";
      del.className = "btn btn-small btn-ghost-danger";
      del.textContent = "Delete";
      del.setAttribute("aria-label", "Delete " + e.hostname);
      del.addEventListener("click", function () { openDelete(e); });
      actions.appendChild(del);
      tr.appendChild(actions);

      rows.appendChild(tr);
    });
  }

  function refresh() {
    return Promise.all([api("GET", "/api/status"), api("GET", "/api/entries")])
      .then(function (results) {
        var status = results[0];
        readOnlyHint = status.writable ? "" : (status.read_only_hint || "The hosts file is read-only.");
        $("footer-hosts").textContent = "Hosts file: " + status.hosts_file;
        if (readOnlyHint) {
          showBanner("Read-only: you can view hosts but not add or delete them. " + readOnlyHint, "warn");
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

  // Add host
  $("add-button").addEventListener("click", function () {
    $("add-form").reset();
    setError($("add-error"), readOnlyHint ? "Read-only: " + readOnlyHint : "");
    addDialog.showModal();
    $("add-hostname").focus();
  });

  $("add-form").addEventListener("submit", function (ev) {
    ev.preventDefault();
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

  // Delete host
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
