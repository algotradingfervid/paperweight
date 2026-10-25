// dialog.js: native <dialog> open and close. [data-open-dialog="<id>"] opens
// the dialog modally; [data-close-dialog] and a click on the backdrop close
// it; focus returns to the opener. HX-Trigger {"dialog:open":"<id>"} opens it too.

const openers = new WeakMap();

function openDialog(id, opener) {
  const dlg = document.getElementById(id);
  if (!dlg || typeof dlg.showModal !== "function" || dlg.open) return;
  openers.set(dlg, opener || document.activeElement);
  dlg.showModal();
}

document.addEventListener("click", function (evt) {
  const t = evt.target;
  if (!(t instanceof Element)) return;
  const opener = t.closest("[data-open-dialog]");
  if (opener) {
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
