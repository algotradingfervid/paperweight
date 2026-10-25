// Print page toolbar (FILE-070). Served from /print/assets/print.js: the
// print CSP forbids inline script.
document.getElementById('print-btn').addEventListener('click', () => window.print());
document.getElementById('close-btn').addEventListener('click', () => window.close());
