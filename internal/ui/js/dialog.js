// dialog.js: native <dialog> open and close. [data-open-dialog="<id>"] opens
// the dialog modally; [data-close-dialog] and a click on the backdrop close
// it; focus returns to the opener. HX-Trigger {"dialog:open":"<id>"} opens it too.
// An opener with data-if-dirty opens it only when its form changed (below).

const openers = new WeakMap();

// [data-if-dirty="<form id>"] on an opener opens its dialog only when that
// form has unsaved changes; otherwise the opener's link is followed (CD-387
// (4)). A form is changed when it carries data-unsaved (a redraw of values
// that weren't saved) or its values differ from when the user first touched
// it (focus, or a pointer where a tap doesn't focus a button), so before any
// typing, picking or tapping a chip.
const drawn = new WeakMap();
const state = (f) => JSON.stringify([...new FormData(f)]);

for (const type of ["focusin", "pointerdown"]) {
  document.addEventListener(type, (evt) => {
    const f = evt.target.form || evt.target.closest?.("form");
    if (f && !drawn.has(f)) drawn.set(f, state(f));
  }, true);
}

// The form always exists beside its ×: Frame draws both.
function unsaved(id) {
  const f = document.getElementById(id);
  return f.hasAttribute("data-unsaved") || (drawn.get(f) ?? state(f)) !== state(f);
}

function openDialog(id, opener) {
  const dlg = document.getElementById(id);
  if (!dlg || typeof dlg.showModal !== "function" || dlg.open) return;
  openers.set(dlg, opener || document.activeElement);
  dlg.showModal();
  // [data-dialog-focus] takes focus on open, where autofocus would also count
  // on the page itself (the drawer's current item).
  const first = dlg.querySelector("[data-dialog-focus]");
  if (first) first.focus();
}

// Enter on a link opener: keydown would click it and open the dialog, and
// the rest of the key would then press the dialog's first button and close it
// at once. So keydown only stops the click, and the key's keyup acts: it opens
// the dialog, or follows a clean data-if-dirty link. Ctrl, Cmd, Shift or Alt
// with Enter is left to the browser.
// A modifier keeps the browser's own link behaviour (new tab or window).
const modified = (evt) => evt.ctrlKey || evt.metaKey || evt.shiftKey || evt.altKey;
let entered = null;
for (const type of ["keydown", "keyup"]) {
  document.addEventListener(type, (evt) => {
    const opener = evt.key === "Enter" && !modified(evt) && evt.target.closest?.("a[data-open-dialog]");
    if (!opener) return;
    evt.preventDefault();
    if (type === "keydown") return void (entered = opener);
    if (opener !== entered) return;
    entered = null;
    const form = opener.getAttribute("data-if-dirty");
    if (form && !unsaved(form)) location.assign(opener.href);
    else openDialog(opener.getAttribute("data-open-dialog"), opener);
  });
}

document.addEventListener("click", function (evt) {
  const t = evt.target;
  if (!(t instanceof Element)) return;
  const opener = t.closest("[data-open-dialog]");
  if (opener) {
    if (opener.href && modified(evt)) return;
    const form = opener.getAttribute("data-if-dirty");
    if (form && !unsaved(form)) return;
    evt.preventDefault();
    openDialog(opener.getAttribute("data-open-dialog"), opener);
    return;
  }
  const closer = t.closest("[data-close-dialog]");
  if (closer) {
    const dlg = closer.closest("dialog");
    if (dlg) dlg.close();
    return;
  }
  // A click on the dialog element itself is a click on its backdrop.
  if (t instanceof HTMLDialogElement && t.open) t.close();
});

// The dialog's close event (button, backdrop or Escape) returns focus.
document.addEventListener("close", function (evt) {
  const dlg = evt.target;
  const opener = openers.get(dlg);
  openers.delete(dlg);
  if (opener && opener.isConnected && typeof opener.focus === "function") opener.focus();
}, true);

document.addEventListener("dialog:open", function (evt) {
  const d = evt.detail;
  const id = typeof d === "string" ? d : d && (d.value || d.id);
  if (id) openDialog(id, null);
});

// The open account <details> closes on Escape, handing focus back to its
// summary, and on a click outside it.
document.addEventListener("keydown", function (evt) {
  const m = document.querySelector("details.user-menu[open]");
  if (evt.key !== "Escape" || !m) return;
  m.open = false;
  m.querySelector("summary").focus();
});

document.addEventListener("click", function (evt) {
  const m = document.querySelector("details.user-menu[open]");
  if (m && !m.contains(evt.target)) m.open = false;
});
