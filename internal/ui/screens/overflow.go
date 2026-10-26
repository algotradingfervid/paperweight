package screens

// OverflowJS returns a []string of horizontal overflow problems on the page.
// It is research frontend-tech.md §6.4 overflowJS, verbatim.
const OverflowJS = `(() => {
  const de = document.documentElement, vw = de.clientWidth, out = [];
  if (de.scrollWidth > vw) out.push("document scrollWidth " + de.scrollWidth + " > clientWidth " + vw + " (innerWidth " + innerWidth + ")");
  const name = el => el.tagName.toLowerCase() + (el.id ? "#" + el.id : "") +
    (typeof el.className === "string" && el.className.trim() ? "." + el.className.trim().split(/\s+/).join(".") : "");
  for (const el of document.querySelectorAll("body *")) {
    if (el.closest("[data-allow-overflow]")) continue;          // escape hatch: must be reviewed (e.g. none in v1)
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.display === "contents") continue;
    const r = el.getBoundingClientRect();
    if (r.width === 0 && r.height === 0) continue;
    if (r.right > vw + 0.5 || r.left < -0.5) { out.push("outside viewport: " + name(el) + " [" + Math.round(r.left) + "," + Math.round(r.right) + "]"); continue; }
    if (el.scrollWidth > el.clientWidth + 1 && el.clientWidth > 0 && cs.textOverflow !== "ellipsis" && !(el instanceof SVGElement))
      out.push("content wider than box: " + name(el) + " scrollWidth " + el.scrollWidth + " > " + el.clientWidth + " overflow-x:" + cs.overflowX);
  }
  return out;
})()`

// TargetSizeJS returns a []string of "tag#id.class WxH" for every visible
// interactive element whose box is smaller than 44 px in either dimension.
// A labelled radio or checkbox is measured by its label, the full-row target
// (CD-169).
const TargetSizeJS = `(() => {
  const out = [];
  const name = el => el.tagName.toLowerCase() + (el.id ? "#" + el.id : "") +
    (typeof el.className === "string" && el.className.trim() ? "." + el.className.trim().split(/\s+/).join(".") : "");
  const sel = 'a[href], button, input:not([type=hidden]), select, textarea, summary, [role=option], [tabindex]:not([tabindex="-1"])';
  for (const el of document.querySelectorAll(sel)) {
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.visibility === "hidden") continue;
    const choice = el.type === "radio" || el.type === "checkbox";
    const r = (choice && el.labels && el.labels.length ? el.labels[0] : el).getBoundingClientRect();
    if (r.width === 0 && r.height === 0) continue;
    if (r.width < 44 || r.height < 44) out.push(name(el) + " " + Math.round(r.width) + "x" + Math.round(r.height));
  }
  return out;
})()`

// CSPReportPath is where e2e builds send CSP violation reports.
const CSPReportPath = "/csp-report"
