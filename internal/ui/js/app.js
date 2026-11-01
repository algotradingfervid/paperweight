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
// in #network (UI v2 §5.14, CD-346). Try again re-sends only a safe request:
// a GET, or a POST carrying a saved record's version, whose duplicate is
// refused with 409. A form post is re-sent by clicking its submit button
// again (so its name and value go too), anything else as the same htmx
// request from the same element. Any other POST may have been saved, so its
// banner offers Reload instead. The next request answered below 500 clears
// the banner; a 4xx never shows it.
const network = document.getElementById("network");
for (const name of ["sendError", "timeout", "responseError", "afterRequest"]) {
  document.addEventListener("htmx:" + name, function (evt) {
    const d = evt.detail;
    const c = d.requestConfig;
    const status = d.xhr.status;
    if (!network) return;
    if (name === "afterRequest") {
      if (status && status < 500) network.replaceChildren();
      return;
    }
    if (status && status < 500) return;
    const safe = c.verb === "get" || Number(c.formData.get("version")) > 0;
    const node = document.getElementById(safe ? (status ? "network-server" : "network-offline") : "network-unsure").content.firstElementChild.cloneNode(true);
    node.querySelector("button").onclick = function () {
      const btn = c.triggeringEvent?.submitter;
      if (!safe) location.reload();
      else if (btn?.isConnected) btn.click();
      else htmx.ajax(c.verb, c.path, { source: d.elt });
    };
    network.replaceChildren(node);
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
