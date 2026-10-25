// field.js: applies the "ui:field" event sent by /ui/validate/{kind} (CD-48).
// The server parses and formats; this only writes back what it sent.
document.addEventListener("ui:field", function (e) {
  const d = e.detail || {};
  const input = document.getElementById(d.id);
  if (!input) return;
  input.value = d.value;
  if (d.invalid) {
    input.setAttribute("aria-invalid", "true");
    input.classList.add("field--error");
  } else {
    input.removeAttribute("aria-invalid");
    input.classList.remove("field--error");
  }
});
