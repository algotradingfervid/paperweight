// date.js: DateInput's "Today" and "Yesterday" chips and its "Choose date"
// button (CD-336 (1)). The button opens the platform picker of a hidden
// date input on the box's date; a picked date is written back as
// dd-mm-yyyy. The change event sends the value to /ui/validate/date.
// "dd-mm-yyyy" and "yyyy-mm-dd" turn into each other by reversing the parts.
const flip = (v) => v.split("-").reverse().join("-");

function setDate(id, v) {
  const input = document.getElementById(id);
  if (!input) return;
  input.value = v;
  input.dispatchEvent(new Event("change", { bubbles: true }));
}

document.addEventListener("click", function (e) {
  const pick = e.target.closest("[data-date-pick]");
  if (pick) {
    const d = pick.nextElementSibling;
    d.value = flip(document.getElementById(pick.getAttribute("aria-controls")).value);
    try {
      d.showPicker();
    } catch {
      d.focus();
      d.click();
    }
    return;
  }
  const btn = e.target.closest("[data-set-date]");
  if (btn) setDate(btn.getAttribute("aria-controls"), btn.dataset.setDate);
});

document.addEventListener("change", function (e) {
  const d = e.target;
  if (d.dataset.datePicker && d.value) setDate(d.dataset.datePicker, flip(d.value));
});
