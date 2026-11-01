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
// comma steps over it, so it removes the digit past it.
document.addEventListener("beforeinput", function (e) {
  const t = e.target, s = t.dataset.scale, signed = "signed" in t.dataset, a = t.selectionStart, b = t.selectionEnd, v = t.value;
  const d = { deleteContentBackward: -1, deleteContentForward: 1 }[e.inputType];
  if (!s) return;
  if (e.inputType === "insertText" && ok(v, s, signed) && (!ok(v.slice(0, a) + e.data + v.slice(b), s, signed) || (e.data === "0" && /^-?$/.test(v.slice(0, a)) && /^\d/.test(v.slice(b))))) e.preventDefault();
  if (d && a === b && v[d < 0 ? a - 1 : a] === ",") t.setSelectionRange(a + d, a + d);
});

// An ok value is regrouped with the caret after the same digit: after as
// many kept characters as came before it.
document.addEventListener("input", function (e) {
  const t = e.target, s = t.dataset.scale;
  if (!s || !ok(t.value, s, "signed" in t.dataset)) return;
  let k = tidy(t.value.slice(0, t.selectionStart)).replace(/,/g, "").length, p = 0;
  t.value = tidy(t.value);
  while (k) if (t.value[p++] !== ",") k--;
  t.setSelectionRange(p, p);
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
