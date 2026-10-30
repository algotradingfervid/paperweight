// upload.js: FileUpload's busy state and refusals (design-system §7; UX design
// 2026-10-31 §6 changes 1 and 2). While a file uploads, the status line says
// its data-busy text and htmx:xhr:progress moves the <progress> (never a
// style). Every request inside the upload clears that when it ends (422 counts
// as a success in the htmx config), and one that ended without a page reload
// returns focus to the input used. A server notice on a form inside the
// upload closes any open dialog around it and shows in the message region,
// cloned from the upload's <template> as a text node; only an upload form's
// refusal marks the inputs. No answer at all shows the data-dropped text.

const used = new WeakMap(); // upload box -> the input that started its upload

// on runs fn for event name from inside an upload, with the box, the
// controls (null for a Remove confirm), the progress bar and the status line.
const on = (name, fn) => document.addEventListener(name, (e) => {
  const b = e.target instanceof Element && e.target.closest(".upload");
  if (b) fn(b, e, e.target.closest(".upload__controls"), b.querySelector("progress"), document.getElementById(b.id + "-status"));
});

// msg shows text in the message region, or clears it when text is null; mark
// sets aria-invalid on both inputs, else it is removed.
const msg = (b, text, mark) => {
  const m = document.getElementById(b.id + "-msg");
  const c = b.querySelector("template").content.cloneNode(true);
  c.querySelector("span").textContent = text;
  m.classList.toggle("field__msg--error", text != null);
  m.replaceChildren(...(text != null ? [c] : []));
  for (const i of b.querySelectorAll(".upload__controls input[type=file]")) {
    if (mark) i.setAttribute("aria-invalid", "true");
    else i.removeAttribute("aria-invalid");
  }
};

on("htmx:beforeRequest", (b, e, up, p, s) => {
  if (!up) return;
  used.set(b, e.target.querySelector("input[type=file]"));
  msg(b, null);
  p.value = 0;
  p.hidden = false;
  s.textContent = s.dataset.busy;
});

on("htmx:xhr:progress", (b, e, up, p) => {
  const d = e.detail;
  if (d.total) {
    p.hidden = false;
    p.value = Math.round(d.loaded / d.total * 100);
  }
});

// htmx 2 fires htmx:afterRequest before htmx:sendError, so this also clears
// a dropped upload. A refresh reloads at the upload's fragment, so the
// browser keeps the scroll and a heading's autofocus doesn't move the page.
// Otherwise the input used is emptied, so picking the same file again (after
// a dropped connection) is a change, and gets focus back.
on("htmx:afterRequest", (b, e, up, p, s) => {
  p.hidden = true;
  p.value = 0;
  s.textContent = "";
  const i = used.get(b);
  used.delete(b);
  if (e.detail.xhr.getResponseHeader("HX-Refresh") === "true") {
    history.replaceState(null, "", "#" + b.id);
    location.reload();
  } else if (i && i.isConnected) {
    i.value = "";
    i.focus();
  }
});

on("htmx:sendError", (b, e, up, p, s) => {
  if (up) msg(b, s.dataset.dropped, false);
});

on("notice", (b, e, up) => {
  const d = e.target.closest("dialog");
  if (d && d.open) d.close();
  msg(b, String(e.detail.text), up);
});
