// counter.js: keeps Textarea's "0 of 1,000 characters" line live, and a
// quantities step's "{n} of {m} lines entered" (GRN CD-387 (7)).
const count = new Intl.NumberFormat("en-IN");

// entered is grnweb's nonZero: there and not 0; what can't be read counts,
// as Save puts its error on that card.
function entered(input) {
  const v = input.value.replaceAll(",", "").trim();
  return v !== "" && Number(v) !== 0;
}

document.addEventListener("input", function (e) {
  const t = e.target;
  if (t instanceof HTMLInputElement && t.hasAttribute("data-line-entry")) {
    const out = document.querySelector("[data-lines-entered]");
    const all = [...document.querySelectorAll("input[data-line-entry]")];
    const text = all.filter(entered).length + " of " + all.length + " lines entered";
    if (out && out.textContent !== text) out.textContent = text;
    return;
  }
  if (!(t instanceof HTMLTextAreaElement) || t.maxLength < 0) return;
  const out = document.querySelector('[data-counter="' + CSS.escape(t.id) + '"]');
  if (!out) return;
  out.textContent = count.format(t.value.length) + " of " + count.format(t.maxLength) + " characters";
});
