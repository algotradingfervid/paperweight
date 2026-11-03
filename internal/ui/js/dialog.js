// dialog.js: native <dialog> open and close. [data-open-dialog="<id>"] opens
// the dialog modally; [data-close-dialog] and a click on the backdrop close
// it; focus returns to the opener. HX-Trigger {"dialog:open":"<id>"} opens it too.
// An opener with data-if-dirty opens it only when its form changed (below).

const openers = new WeakMap();

// [data-if-dirty="<form id>"] on an opener opens its dialog only when that
// form has unsaved changes; otherwise the opener's link is followed (CD-387
// (4)). A form is changed when it carries data-unsaved (a redraw of values
// that weren't saved) or its values differ from when the user first touched
// it (focus, pointer or key, so before any typing, picking or tapping a chip,
// also where a tap doesn't focus a button).
const drawn = new WeakMap();

function formState(form) {
  const parts = [];
  for (const [k, v] of new FormData(form)) {
    parts.push(k + "=" + (typeof v === "string" ? v : v.name + ":" + v.size));
  }
  return parts.join("&");
}

function touched(evt) {
  const t = evt.target;
  if (!(t instanceof Element)) return;
  const form = t.form || t.closest("form");
  if (form && !drawn.has(form)) drawn.set(form, formState(form));
}
for (const type of ["focusin", "pointerdown", "keydown"]) document.addEventListener(type, touched, true);

function unsaved(id) {
  const form = document.getElementById(id);
  if (!(form instanceof HTMLFormElement)) return false;
  if (form.hasAttribute("data-unsaved")) return true;
  return drawn.has(form) && drawn.get(form) !== formState(form);
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

document.addEventListener("click", function (evt) {
  const t = evt.target;
  if (!(t instanceof Element)) return;
  const opener = t.closest("[data-open-dialog]");
  if (opener) {
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
