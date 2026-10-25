// toast.js: server notices (HX-Trigger {"notice":{"level","text"}}, CD-75).
// Success and info are toasts in #toasts that leave after 5 s (paused while
// hovered or focused). Errors and warnings are never toasts: they go to the
// persistent #alerts region and stay until closed (CD-47). Text is set with
// textContent, never as markup.

const LEVELS = { success: "alert-success", info: "alert-info", warning: "alert-warning", error: "alert-error" };
const DISMISS_MS = 5000;

function closeButton(node) {
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = "btn btn-ghost btn-square";
  btn.setAttribute("aria-label", "Dismiss");
  btn.textContent = "×";
  btn.addEventListener("click", function () { node.remove(); });
  return btn;
}

function autoDismiss(node) {
  let timer = 0;
  function start() { timer = setTimeout(function () { node.remove(); }, DISMISS_MS); }
  function stop() { clearTimeout(timer); }
  node.addEventListener("mouseenter", stop);
  node.addEventListener("focusin", stop);
  node.addEventListener("mouseleave", start);
  node.addEventListener("focusout", function (evt) {
    if (!node.contains(evt.relatedTarget)) start();
  });
  start();
}

function show(level, text) {
  const cls = LEVELS[level] || LEVELS.error;
  const toast = level === "success" || level === "info";
  const region = document.getElementById(toast ? "toasts" : "alerts");
  if (!region) return;
  const node = document.createElement("div");
  node.className = "alert " + cls;
  const p = document.createElement("p");
  p.textContent = String(text == null ? "" : text);
  node.appendChild(p);
  node.appendChild(closeButton(node));
  region.appendChild(node);
  if (toast) autoDismiss(node);
}

document.addEventListener("notice", function (evt) {
  const d = evt.detail || {};
  show(d.level, d.text);
});
