// counter.js: keeps Textarea's "0 of 1,000 characters" line live.
const count = new Intl.NumberFormat("en-IN");

document.addEventListener("input", function (e) {
  const t = e.target;
  if (!(t instanceof HTMLTextAreaElement) || t.maxLength < 0) return;
  const out = document.querySelector('[data-counter="' + CSS.escape(t.id) + '"]');
  if (!out) return;
  out.textContent = count.format(t.value.length) + " of " + count.format(t.maxLength) + " characters";
});
