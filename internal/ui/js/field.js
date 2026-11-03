// field.js: number inputs group digits as typed (CD-336 (2), CD-355), and
// every checked input takes the "ui:field" event sent by /ui/validate/{kind}
// (CD-48). The server parses and formats; this only groups, and writes back
// what it sent.

// ok is whether v holds only digits, grouping commas and spaces, then at most
// scale decimals after one point and trailing spaces, with a leading minus
// only when signed.
// Anything else (a letter, "₹", a comma after the point) is left for NUM to
// refuse (CD-355 (2)).
const ok = (v, scale, signed) => RegExp("^" + (signed ? "-?" : "") + "[\\d,\\s]*" + (+scale ? "(\\.\\d{0," + scale + "})?" : "") + "\\s*$").test(v);

// tidy is an ok value grouped Indian style without leading zeros (one kept
// before the point): "-0150000.5" is "-1,50,000.5".
const tidy = (v) => {
  const [i, f] = v.replace(/[,\s]/g, "").split(".");
  return i.replace(/^(-?)0+(?=\d)/, "$1").replace(/(\d)(?=(\d\d)*\d{3}$)/g, "$1,") + (f === undefined ? "" : "." + f);
};

// A typed character that would make an ok value not ok is dropped, and so is
// a zero typed in front of the first digit (CD-355 (3)); a delete beside a
// comma, or a date's dash (CD-387 (9)), with more after it steps over it, so
// it removes the digit past it.
document.addEventListener("beforeinput", function (e) {
  const t = e.target, s = t.dataset.scale, signed = "signed" in t.dataset, a = t.selectionStart, b = t.selectionEnd, v = t.value;
  const d = { deleteContentBackward: -1, deleteContentForward: 1 }[e.inputType];
  const sep = s ? "," : "date" in t.dataset && "-";
  if (s && e.inputType === "insertText" && ok(v, s, signed) && (!ok(v.slice(0, a) + e.data + v.slice(b), s, signed) || (e.data === "0" && /^-?$/.test(v.slice(0, a)) && /^\d/.test(v.slice(b))))) e.preventDefault();
  if (d && a === b && v[d < 0 ? a - 1 : a] === sep && v[d < 0 ? a : a + 1]) t.setSelectionRange(a + d, a + d);
});

// rewrite gives input t value v with the caret after k characters that aren't
// sep, and past a sep that ends the value.
export const rewrite = (t, v, k, sep) => {
  let p = 0;
  t.value = v;
  while (k) if (v[p++] !== sep) k--;
  if (p === v.length - 1 && v[p] === sep) p++;
  t.setSelectionRange(p, p);
};

// An ok value is regrouped with the caret after the same digit: after as
// many kept characters as came before it.
document.addEventListener("input", function (e) {
  const t = e.target, s = t.dataset.scale;
  if (!s || !ok(t.value, s, "signed" in t.dataset)) return;
  rewrite(t, tidy(t.value), tidy(t.value.slice(0, t.selectionStart)).replace(/,/g, "").length, ",");
});

// The first tap on a prefilled number or date selects it, so the first digit
// replaces it wherever the finger landed (CD-387 (8)). That runs on click,
// because the press itself places the caret, and leaves a drag's selection
// alone. A tap on the selected value collapses the selection first: the browser
// then places the caret at the tap, which it won't do inside a selection. That
// waits for pointerup, which a scroll starting on the field never fires, so a
// scroll leaves the selection for the next digit to replace.
let fresh;
document.addEventListener("pointerdown", function (e) {
  fresh = e.target !== document.activeElement;
});
document.addEventListener("pointerup", function (e) {
  const t = e.target;
  if (!fresh && "tapSelect" in t.dataset && t.value && t.selectionEnd - t.selectionStart === t.value.length) t.setSelectionRange(0, 0);
});
document.addEventListener("click", function (e) {
  const t = e.target;
  if (fresh && "tapSelect" in t.dataset && t.selectionStart === t.selectionEnd) t.setSelectionRange(0, t.value.length);
});

// The reply echoes the value its request carried: it is written back only
// while the input still holds exactly that, so it never overwrites a later
// edit (CD-355 (1)).
document.addEventListener("ui:field", function (e) {
  const d = e.detail || {};
  const input = document.getElementById(d.id);
  if (!input) return;
  if (input.value === d.sent) input.value = d.value;
  if (d.invalid) {
    input.setAttribute("aria-invalid", "true");
    input.classList.add("field--error");
  } else {
    input.removeAttribute("aria-invalid");
    input.classList.remove("field--error");
  }
});
