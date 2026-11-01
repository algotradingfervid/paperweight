// date.js: DateInput's "Today" and "Yesterday" chips and its "Choose date"
// button (CD-336 (1)). The button opens the platform picker of a hidden
// date input on the box's date; a picked date is written back as
// dd-mm-yyyy. The change event sends the value to /ui/validate/date.
// toISO is a typed d-m-yyyy, d/m/yyyy or d.m.yyyy date (one separator, as
// the server reads it) as yyyy-mm-dd, or "" (the picker then opens on today)
// for anything else (CD-351 (1)).
const toISO = (v) => {
  const m = /^(\d\d?)([-/.])(\d\d?)\2(\d{4})$/.exec(v.trim());
  return m ? m[4] + "-" + m[3].padStart(2, "0") + "-" + m[1].padStart(2, "0") : "";
};

function setDate(id, v) {
  const input = document.getElementById(id);
  if (!input) return;
  input.value = v;
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
