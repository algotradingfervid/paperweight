// date.js: the "Today" and "Yesterday" buttons of DateInput. The server
// renders the dates; the change event sends the value to /ui/validate/date.
document.addEventListener("click", function (e) {
  const btn = e.target.closest("[data-set-date]");
  if (!btn) return;
  const input = document.getElementById(btn.getAttribute("aria-controls"));
  if (!input) return;
  input.value = btn.dataset.setDate;
  input.dispatchEvent(new Event("change", { bubbles: true }));
});
