// busy.js: a submitting button is disabled and shows its data-busy-label
// until the htmx request ends, so a double tap sends one request.

function setBusy(btn) {
  if (!btn || !btn.dataset.busyLabel || btn.disabled) return;
  btn.dataset.idleLabel = btn.textContent;
  btn.textContent = btn.dataset.busyLabel;
  btn.disabled = true;
}

function clearBusy(btn) {
  if (!btn || btn.dataset.idleLabel === undefined) return;
  btn.textContent = btn.dataset.idleLabel;
  delete btn.dataset.idleLabel;
  btn.disabled = false;
}

function submitter(evt) {
  const d = evt.detail || {};
  if (d.requestConfig && d.requestConfig.triggeringEvent && d.requestConfig.triggeringEvent.submitter) {
    return d.requestConfig.triggeringEvent.submitter;
  }
  const elt = d.elt;
  if (elt && elt.matches && elt.matches("button, input[type=submit]")) return elt;
  return null;
}

// A plain form post: disable after the event has run, so the button's own
// name and value are still sent.
document.addEventListener("submit", function (evt) {
  if (evt.defaultPrevented) return;
  const btn = evt.submitter;
  setTimeout(function () { setBusy(btn); }, 0);
});

document.addEventListener("htmx:beforeRequest", function (evt) {
  setBusy(submitter(evt));
});

document.addEventListener("htmx:afterRequest", function (evt) {
  clearBusy(submitter(evt));
});

// Back to a page kept in the back/forward cache: its buttons work again.
window.addEventListener("pageshow", function (evt) {
  if (!evt.persisted) return;
  document.querySelectorAll("[data-idle-label]").forEach(clearBusy);
});
