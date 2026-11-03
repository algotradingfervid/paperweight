// Print page toolbar (FILE-070). Served from /print/assets/print.js: the
// print CSP forbids inline script.
document.getElementById('print-btn').addEventListener('click', () => window.print());
document.getElementById('close-btn').addEventListener('click', () => window.close());
// On a narrow screen the lines scroll in their own box (print.css), so a
// keyboard must reach it. Attributes only: nothing here reaches paper.
document.querySelectorAll('table.lines').forEach((t) => {
  t.tabIndex = 0;
  t.setAttribute('aria-label', 'Lines, scrolls sideways');
  t.setAttribute('data-allow-overflow', '');
});
