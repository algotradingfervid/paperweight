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
// in #network (UI v2 §5.14). Try again re-issues the request: a form post by
// clicking its submit button again (so its name and value go too), anything
// else as the same htmx request from the same element. The next request
// answered below 500 clears the banner.
const network = document.getElementById("network");
for (const name of ["sendError", "timeout", "responseError", "afterRequest"]) {
  document.addEventListener("htmx:" + name, function (evt) {
    const d = evt.detail;
    const status = d.xhr.status;
    if (!network) return;
    if (name === "afterRequest") {
      if (status && status < 500) network.replaceChildren();
      return;
    }
    if (status && status < 500) return;
    const node = document.getElementById(status ? "network-server" : "network-offline").content.firstElementChild.cloneNode(true);
    node.querySelector("button").onclick = function () {
      const btn = d.requestConfig.triggeringEvent?.submitter;
      if (btn?.isConnected) btn.click();
      else htmx.ajax(d.requestConfig.verb, d.requestConfig.path, { source: d.elt });
    };
    network.replaceChildren(node);
  });
}

// "/" focuses the header search, unless the user is typing in a field.
document.addEventListener("keydown", function (evt) {
  const search = document.querySelector("header [role=search] input");
  if (evt.key !== "/" || evt.ctrlKey || evt.metaKey || !search || evt.target.closest?.("input, textarea, select")) return;
  evt.preventDefault();
  search.focus();
});
