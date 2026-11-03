// date.js: DateInput's typed box, "Today" and "Yesterday" chips and "Choose
// date" button (CD-336 (1)). The button opens the platform picker of a hidden
// date input on the box's date; a picked date is written back as
// dd-mm-yyyy. The change event sends the value to /ui/validate/date.
// toISO is a typed d-m-yyyy, d/m/yyyy or d.m.yyyy date (one separator, as
// the server reads it) as yyyy-mm-dd, or "" (the picker then opens on today)
// for anything else (CD-351 (1)).
import { rewrite } from "./field.js";

const toISO = (v) => {
  const m = /^(\d\d?)([-/.])(\d\d?)\2(\d{4})$/.exec(v.trim());
  return m ? m[4] + "-" + m[3].padStart(2, "0") + "-" + m[1].padStart(2, "0") : "";
};

// dashed is a typed value of day, month and year digits with its dashes
// (CD-387 (9)): "0511" is "05-11". A month starts 0 or 1, so a year typed first
// ("2026-10-24") stays; one that reads as a day and month ("2010") is put back
// by the dash after its month ("20-10-04-" is "2010-04-"). Anything else stays
// as typed for the server to read or refuse (NUM-078).
const dashed = (v) => {
  const m = /^(\d\d)(?:-?([01]\d?)(?:-?(\d{1,4}))?)?$/.exec(v.trim());
  return m ? m.slice(1).filter(Boolean).join("-") : v.replace(/^(\d\d)-(\d\d)(?=-\d\d-)/, "$1$2");
};

// press keeps aria-pressed on box t's chips: true on the one holding its date.
const press = (t) => {
  for (const c of document.querySelectorAll('[data-set-date][aria-controls="' + t.id + '"]')) c.setAttribute("aria-pressed", toISO(c.dataset.setDate) === toISO(t.value));
};

function setDate(id, v) {
  const input = document.getElementById(id);
  if (!input) return;
  input.value = v;
  press(input);
  input.dispatchEvent(new Event("change", { bubbles: true }));
}

document.addEventListener("click", function (e) {
  const pick = e.target.closest("[data-date-pick]");
  if (pick) {
    const id = pick.getAttribute("aria-controls");
    const d = document.querySelector('[data-date-picker="' + id + '"]');
    d.value = toISO(document.getElementById(id).value);
    try {
      d.showPicker();
    } catch {
      d.focus();
      d.click();
      pick.focus();
    }
    return;
  }
  const btn = e.target.closest("[data-set-date]");
  if (btn) setDate(btn.getAttribute("aria-controls"), btn.dataset.setDate);
});

document.addEventListener("change", function (e) {
  const d = e.target;
  if (d.dataset.datePicker && d.value) setDate(d.dataset.datePicker, d.value.split("-").reverse().join("-"));
});

// A key press over a selection holding a dash can leave digits dashed's forms
// don't fit, so they are regrouped as dd-mm-yyyy; a paste keeps its own form.
let over;
document.addEventListener("beforeinput", function (e) {
  const t = e.target;
  over = "date" in t.dataset && /^(insertText|delete)/.test(e.inputType) && t.value.slice(t.selectionStart, t.selectionEnd).includes("-");
});

// Digits take their dashes with the caret after the same digit.
document.addEventListener("input", function (e) {
  const t = e.target, v = t.value;
  if (!("date" in t.dataset)) return;
  let w = dashed(v);
  const n = v.replace(/-/g, "");
  if (over && w === v && /^\d{1,8}$/.test(n)) w = [n.slice(0, 2), n.slice(2, 4), n.slice(4)].filter(Boolean).join("-");
  if (w !== v) rewrite(t, w, v.slice(0, t.selectionStart).replace(/\D/g, "").length, "-");
  press(t);
});

// The server's writeback ("24-Oct-2026" as 24-10-2026) reads for the chips;
// field.js has written it by now.
document.addEventListener("ui:field", function (e) {
  const t = document.getElementById((e.detail || {}).id);
  if (t && "date" in t.dataset) press(t);
});
