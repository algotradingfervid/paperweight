// Print page toolbar (FILE-070). Served from /print/assets/print.js: the
// print CSP forbids inline script.
document.getElementById('print-btn').addEventListener('click', () => window.print());
document.getElementById('close-btn').addEventListener('click', () => window.close());
// Below 48rem the lines stack, each cell after its column's name
// (print.css). Where they still scroll in their own box, a keyboard must
// reach it; any turn or resize of the screen checks again. Attributes only:
// nothing here reaches paper.
const scrollBoxes = () => document.querySelectorAll('table.lines').forEach((t) => {
  if (t.scrollWidth <= t.clientWidth) {
    t.removeAttribute('tabindex');
    t.removeAttribute('aria-label');
    t.removeAttribute('data-allow-overflow');
    return;
  }
  t.tabIndex = 0;
  t.setAttribute('aria-label', 'Lines, scrolls sideways');
  t.setAttribute('data-allow-overflow', '');
});
scrollBoxes();
addEventListener('resize', scrollBoxes);
