// toast.js: server notices (HX-Trigger {"notice":{"level","text"}}, CD-75).
// Success and info are toasts in #toasts that leave after 5 s (paused while
// hovered or focused). Errors and warnings are never toasts: they go to the
// persistent #alerts region and stay until closed (CD-47). Each notice is a
// copy of its <template id="notice-KIND"> (components.NoticeTemplates), the
// same markup as the server's components.Toast; toasts htmx adds out of band
// get the same setup. Text is set as a text node, never as markup.

const KINDS = { success: "success", info: "info", warning: "warning", error: "danger" };
const DISMISS_MS = 5000;
const wired = new WeakSet();

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

function wire(node, toast) {
  if (wired.has(node)) return;
  wired.add(node);
  const btn = node.querySelector("[data-dismiss]");
  if (btn) btn.addEventListener("click", function () { node.remove(); });
  if (toast) autoDismiss(node);
}

function show(level, text) {
  const kind = KINDS[level] || KINDS.error;
  const toast = kind === "success" || kind === "info";
  const region = document.getElementById(toast ? "toasts" : "alerts");
  const tpl = document.getElementById("notice-" + kind);
  if (!region || !tpl) return;
  const node = tpl.content.firstElementChild.cloneNode(true);
  node.querySelector(".banner-body p").append(" " + String(text == null ? "" : text));
  region.appendChild(node);
  wire(node, toast);
}

document.addEventListener("notice", function (evt) {
  const d = evt.detail || {};
  show(d.level, d.text);
});

document.addEventListener("htmx:oobAfterSwap", function () {
  const region = document.getElementById("toasts");
  if (!region) return;
  for (const node of region.children) wire(node, true);
});
