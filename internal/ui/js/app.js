// app.js: the only JS entry point, bundled by cmd/buildassets with esbuild.
// Each file below is owned by one task (docs/superpowers/plans/2026-10-24-wp1-ui.md).
import "./busy.js";
import "./dialog.js";
import "./toast.js";
import "./field.js";
import "./date.js";
import "./counter.js";
import "./password.js";
import "./upload.js";
import "./alpine/combobox.js";
import "./alpine/linegrid.js";

// A request with no answer (htmx sendError, timeout) or a 5xx shows a banner
// in #network (UI v2 §5.14, CD-346, CD-387 (5)), sticky under the head. It
// takes focus, unless a background request failed while the user is in a
// field. Try again re-sends only a safe request: a GET, a POST carrying a
// saved record's version, whose duplicate is refused with 409, a form or
// button that writes nothing (data-no-write, as on a master wizard step, which
// writes only the session), or anything sent while the browser reported
// offline, when nothing left. A form post is re-sent by clicking its submit
// button again (so its name and value go too), anything else as the same htmx
// request from the same element. Any other POST may have been saved, so its
// banner offers Reload instead. Offline is read when the request is sent:
// failing later says nothing about what the server got. Reload's banner goes
// only when every request that failed unsafely (method and path) has had a
// 2xx, and no safe banner replaces it; any other banner goes with any answer
// below 500 but a field's check. A 4xx never shows a banner. In an open dialog
// the banner goes inside it, because #network sits behind a modal, and comes
// home when the dialog closes or is swapped away. An upload says its own
// failure beside the file (upload.js): its input is empty by now, so nothing
// here could re-send it. A field's blur check is only advice, and must not
// take the banner from the Save that the same tap sent.
const network = document.getElementById("network");
const failed = new Set(); // method and path of each request that may have saved, until its 2xx
let shown; // the banner up
document.addEventListener("close", function (evt) {
  if (evt.target.contains(shown)) network.replaceChildren(shown);
}, true);
for (const name of ["beforeSend", "sendError", "timeout", "responseError", "afterRequest"]) {
  document.addEventListener("htmx:" + name, function (evt) {
    const d = evt.detail;
    const c = d.requestConfig;
    const status = d.xhr.status;
    const answered = status && status < 500;
    const key = c.verb + c.path; // the same form posts the same path, query and all
    const check = key.includes("/validate/");
    if (!network) return;
    if (name === "beforeSend") return (d.xhr.off = !navigator.onLine); // a listener's return value is ignored
    if (name === "afterRequest") {
      if (answered && status < 300) failed.delete(key); // only a real 2xx: no answer is 0
      return (failed.size ? shown.isConnected || network.prepend(shown) : answered && !check && shown?.remove());
    }
    if (answered || check || d.elt.closest(".upload__controls")) return;
    const btn = c.triggeringEvent?.submitter;
    const safe = c.verb === "get" || c.formData.get("version") > 0 ||
      (btn || d.elt).closest("[data-no-write]") || (!status && d.xhr.off);
    if (safe && failed.size) return; // a background failure never hides a lost save
    if (!safe) failed.add(key);
    const node = document.getElementById(safe ? (status ? "network-server" : "network-offline") : "network-unsure").content.firstElementChild.cloneNode(true);
    node.querySelector("button").onclick = function () {
      if (!safe) location.reload();
      else if (btn?.isConnected) btn.click();
      else htmx.ajax(c.verb, c.path, { source: d.elt });
    };
    shown?.remove();
    shown = node;
    const box = document.querySelector("[open] .modal-box");
    (box || network).prepend(node);
    node.tabIndex = -1;
    if (btn || !document.activeElement.matches("input,select,textarea")) node.focus();
  });
}

// "/" focuses the header search, unless the user is typing in a field or the
// search is hidden (below 64rem).
document.addEventListener("keydown", function (evt) {
  const search = document.querySelector("header [role=search] input");
  if (evt.key !== "/" || evt.ctrlKey || evt.metaKey || !search?.offsetParent || evt.target.closest?.("input, textarea, select, [contenteditable]")) return;
  evt.preventDefault();
  search.focus();
});

// A swapped card asking for it (Card FocusHeading, CD-358) takes focus on
// its heading once htmx has settled the swap.
document.addEventListener("htmx:afterSettle", function (evt) {
  evt.target.querySelector?.("[data-focus-heading]")?.focus();
});

// A step save's HX-Trigger saved (wizard.Respond) writes "Saved {HH:MM}"
// into the save cue's live region; hx-preserve keeps it across the swap.
addEventListener("saved", function (evt) {
  document.getElementById("saved-at")?.replaceChildren(evt.detail.value);
});


// A focused wizard field the sticky footer or wizard head covers scrolls
// clear of them: "nearest" honours html's scroll-padding-block, which
// focus() alone ignores when the field is inside the viewport (A-12,
// CD-374: measured in TestE2E_FooterNeverCoversFields).
addEventListener("focusin", function (evt) {
  evt.target.closest("#wizard .field")?.scrollIntoView({ block: "nearest" });
});
