// upload.js: FileUpload's busy state and refusals (design-system §7; UX design
// 2026-10-31 §6 changes 1 and 2). While a file uploads, the status line says
// its data-busy text with the file's name (as text, in a region announced
// once) and htmx:xhr:progress moves the <progress> (never a style) and the
// percent beside the status line, which screen readers skip. Every request
// inside the upload clears that when it ends (422 counts
// as a success in the htmx config), and one that ended without a page reload
// returns focus to the input used unless focus is in another field. A
// server notice on a form inside the upload closes any open dialog around it
// and shows in the message region, cloned from the upload's <template> as a
// text node; only an upload form's refusal marks the inputs. No answer at all
// shows the data-dropped text, which #network leaves to it. The next request
// from the upload removes its earlier notice from #alerts. A 2xx answer to an
// upload, unless it is an
// HX-Redirect (the page is leaving), puts the status line's data-done text,
// when it has one, where the busy text was, for screen readers only: the file
// shown below says it on screen. It stays until the next request in the
// upload ends or the next upload starts.

const used = new WeakMap(); // upload box -> the input that started its upload

// say puts text in the status line s, clipped to screen readers only when clip
// is true. Both change in one step, so the region changes once.
const say = (s, text, clip = false) => {
  s.toggleAttribute("data-clipped", clip);
  s.textContent = text;
};

// percent shows text beside the status line s, in a span made at first use.
// It stays outside that role="status" region and out of the accessibility
// tree, so its ticks are never announced.
const percent = (s, text) => {
  let n = s.nextElementSibling;
  if (!n.matches(".upload__percent")) {
    n = document.createElement("span");
    n.className = "upload__percent";
    n.setAttribute("aria-hidden", "true");
    s.after(n);
  }
  n.textContent = text;
};

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
  b.alert?.remove();
  if (!up) return;
  const i = e.target.querySelector("input[type=file]");
  used.set(b, i);
  msg(b, null);
  p.value = 0;
  p.hidden = false;
  say(s, s.dataset.busy.replace("…", () => " " + i.files[0].name + "…"));
  percent(s, "0%");
});

on("htmx:xhr:progress", (b, e, up, p, s) => {
  const d = e.detail;
  if (d.total) {
    p.hidden = false;
    p.value = Math.round(d.loaded / d.total * 100);
    if (up) percent(s, p.value + "%");
  }
});

// A refresh reloads at the upload's fragment, so the browser keeps the
// scroll and a heading's autofocus doesn't move the page; htmx fires
// beforeOnLoad before its own HX-Refresh reload.
on("htmx:beforeOnLoad", (b, e) => {
  if (e.detail.xhr.getResponseHeader("HX-Refresh") === "true") history.replaceState(history.state, "", "#" + b.id);
});

// htmx 2 fires htmx:afterRequest before htmx:sendError, so this also clears
// a dropped upload. Without a refresh the input used is emptied, so picking
// the same file again (after a dropped connection) is a change. Focus goes
// back to it, or to the Change of the file shown when that folded the
// buttons away, unless the user moved it into a field outside the upload,
// so typing there is never interrupted (UX-E5).
on("htmx:afterRequest", (b, e, up, p, s) => {
  const x = e.detail.xhr;
  const st = x.status;
  p.hidden = true;
  p.value = 0;
  say(s, "");
  percent(s, "");
  // No answer, or a 5xx with no notice of its own (a proxy's): #network stays
  // out of it (app.js), so this is the only word on it.
  if (up && (!st || (st >= 500 && !x.getResponseHeader("HX-Trigger")))) msg(b, s.dataset.dropped, false);
  const i = used.get(b);
  used.delete(b);
  if (i && i.isConnected && x.getResponseHeader("HX-Refresh") !== "true") {
    i.value = "";
    if (s.dataset.done && st >= 200 && st < 300 && x.getResponseHeader("HX-Redirect") === null) say(s, s.dataset.done, true);
    const a = document.activeElement;
    // A file shown with Change folds the buttons (RR1a): focus goes to it.
    const to = i.getClientRects().length > 0 ? i : document.querySelector(e.target.getAttribute("hx-target") + " summary") || i;
    if (b.contains(a) || !a.matches("input,select,textarea")) to.focus();
  }
});

on("notice", (b, e, up) => {
  const d = e.target.closest("dialog");
  if (d && d.open) d.close();
  msg(b, String(e.detail.text), up);
  b.alert = document.getElementById("alerts").lastElementChild; // toast.js ran first
});
