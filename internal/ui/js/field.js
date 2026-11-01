// field.js: number inputs group digits as typed (CD-336 (2)), and every
// checked input takes the "ui:field" event sent by /ui/validate/{kind}
// (CD-48). The server parses and formats; this only groups, and writes back
// what it sent.

// tidy is v with only digits, one point, at most scale decimals and, when
// signed, a leading minus, grouped Indian style: "-150000.5" is "-1,50,000.5".
const tidy = (v, scale, signed) => {
  v = v.replace(/[^\d.-]/g, "");
  const [i, ...f] = v.replace(/-/g, "").split(".");
  return (signed && v[0] === "-" ? "-" : "") + i.replace(/(\d)(?=(\d\d)*\d{3}$)/g, "$1,") + (f.length && +scale ? "." + f.join("").slice(0, scale) : "");
};

// A delete beside a comma steps over it, so it removes the digit past it.
document.addEventListener("beforeinput", function (e) {
  const t = e.target, c = t.selectionStart, d = { deleteContentBackward: -1, deleteContentForward: 1 }[e.inputType];
  if (d && t.dataset.scale && c === t.selectionEnd && t.value[d < 0 ? c - 1 : c] === ",") t.setSelectionRange(c + d, c + d);
});

// The caret stays after the same digit: after as many kept characters as
// came before it.
document.addEventListener("input", function (e) {
  const t = e.target, s = t.dataset.scale, signed = "signed" in t.dataset;
  if (!s) return;
  let k = tidy(t.value.slice(0, t.selectionStart), s, signed).replace(/,/g, "").length, p = 0;
  t.value = tidy(t.value, s, signed);
  while (k) if (t.value[p++] !== ",") k--;
  t.setSelectionRange(p, p);
});

// The value a change sent for checking: a reply never overwrites what was
// typed after it, even when htmx issues the request later.
document.addEventListener("change", function (e) {
  e.target.sent = e.target.value;
});

document.addEventListener("ui:field", function (e) {
  const d = e.detail || {};
  const input = document.getElementById(d.id);
  if (!input) return;
  if (input.value === input.sent) input.value = d.value;
  if (d.invalid) {
    input.setAttribute("aria-invalid", "true");
    input.classList.add("field--error");
  } else {
    input.removeAttribute("aria-invalid");
    input.classList.remove("field--error");
  }
});
