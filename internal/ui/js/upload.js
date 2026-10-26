// upload.js: FileUpload's progress bar. htmx fires htmx:xhr:progress on the
// uploading form; this sets the <progress> value (never a style).

function bar(evt) {
  const box = evt.target instanceof Element ? evt.target.closest(".upload") : null;
  return box ? box.querySelector(".upload__progress") : null;
}

document.addEventListener("htmx:xhr:progress", function (evt) {
  const p = bar(evt);
  const d = evt.detail || {};
  if (!p || !d.lengthComputable || !d.total) return;
  p.hidden = false;
  p.value = Math.round((d.loaded / d.total) * 100);
});

// A failed upload keeps the controls; hide the bar again.
document.addEventListener("htmx:afterRequest", function (evt) {
  const p = bar(evt);
  if (!p || (evt.detail && evt.detail.successful)) return;
  p.hidden = true;
  p.value = 0;
});
