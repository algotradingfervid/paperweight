// combobox.js: the Picker's ARIA 1.2 combobox (x-data="combobox"). htmx
// fills the listbox; this moves aria-activedescendant with the arrow keys,
// writes the chosen id and summary on Enter or click, and closes on Esc.
// After each search it puts the Recent group (the template's) above the
// rows, keeping recent picks whose label holds the typed text, marked, and
// dropping the same rows from the search's; arrows move across both groups.
// Closing after "Change" without a new choice shows the old summary again.
// "Clear" empties the value and shows the closed search; focus goes to the
// picker itself, so the phone panel doesn't open over the page.
// Every handler is bound in init(), so the markup holds no expressions.

function combobox() {
  return {
    init() {
      const root = this.$el;
      const input = root.querySelector("[role=combobox]");
      const list = root.querySelector("[role=listbox]");
      const search = root.querySelector("[data-picker-search]");
      const chosen = root.querySelector("[data-picker-chosen]");
      const summary = root.querySelector("[data-picker-summary]");
      const value = root.querySelector("[data-picker-value]");
      const change = root.querySelector("[data-picker-change]");
      const recent = root.querySelector("template[data-picker-recent]");
      let active = -1;

      // addRecent puts the recent picks whose label holds the typed text
      // above the rows, the text marked, and drops them from the search's
      // (picker.css hides a "No matches" row under them).
      function addRecent() {
        const q = input.value.trim().toLowerCase(), g = recent.content.firstElementChild.cloneNode(true);
        for (const o of g.querySelectorAll("[role=option]")) {
          if (o.dataset.label.toLowerCase().includes(q)) {
            for (const el of o.querySelectorAll("span")) {
              // The match is found in the lower-cased text; it is marked only
              // if the same span of the original text matches, so a letter
              // whose lower case is longer never shifts the mark.
              const t = el.textContent, i = t.toLowerCase().indexOf(q);
              if (q && i >= 0 && t.slice(i, i + q.length).toLowerCase() === q) {
                const m = document.createElement("mark");
                m.append(t.slice(i, i + q.length));
                el.replaceChildren(t.slice(0, i), m, t.slice(i + q.length));
              }
            }
            list.querySelector('[data-value="' + o.dataset.value + '"]')?.remove();
          } else o.remove();
        }
        if (g.querySelector("[role=option]")) list.prepend(g);
      }

      function options() {
        return Array.from(list.querySelectorAll("[role=option]:not([aria-disabled=true])"));
      }

      function setActive(i) {
        const opts = options();
        active = i;
        opts.forEach(function (o, j) { o.setAttribute("aria-selected", j === i ? "true" : "false"); });
        if (opts[i]) {
          input.setAttribute("aria-activedescendant", opts[i].id);
          opts[i].scrollIntoView({ block: "nearest" });
        } else {
          input.removeAttribute("aria-activedescendant");
        }
      }

      // On phones the open search is a full-screen panel (CD-133): the
      // page behind it is inert and Tab stays inside it. Live regions stay
      // live, so notices are still announced.
      let inerted = [];

      function sheet() {
        return search.hasAttribute("data-open") && getComputedStyle(search).position === "fixed";
      }

      function setInert(on) {
        if (!on) {
          inerted.forEach(function (el) { el.inert = false; });
          inerted = [];
          return;
        }
        if (inerted.length || !sheet()) return;
        // The field keeps its label, hint and message, which name the input.
        const start = root.closest(".field") || root;
        for (let el = start; el.parentElement && el !== document.body; el = el.parentElement) {
          for (const sib of el.parentElement.children) {
            if (sib !== el && !sib.inert && !sib.matches("[role=alert], [role=status]")) {
              sib.inert = true;
              inerted.push(sib);
            }
          }
        }
      }

      // Inert is always released: when the picker is removed (destroy) and
      // when its form sends a request that may swap it away (the picker's
      // own searches don't count).
      const release = function () { setInert(false); };
      const onRequest = function (e) { if (!root.contains(e.target)) release(); };
      const form = root.closest("form");
      if (form) form.addEventListener("htmx:beforeRequest", onRequest);
      this.cleanup = function () {
        release();
        if (form) form.removeEventListener("htmx:beforeRequest", onRequest);
      };

      function setOpen(open) {
        input.setAttribute("aria-expanded", open ? "true" : "false");
        search.toggleAttribute("data-open", open);
        setInert(open);
        if (!open) setActive(-1);
      }

      search.addEventListener("keydown", function (e) {
        if (e.key !== "Tab" || !sheet()) return;
        const els = Array.from(search.querySelectorAll("input, button")).filter(function (el) { return el.getClientRects().length > 0; });
        const first = els[0], last = els[els.length - 1];
        if (e.shiftKey && document.activeElement === first) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && document.activeElement === last) {
          e.preventDefault();
          first.focus();
        }
      });

      // close shuts the list; with a value already chosen it shows that
      // summary again instead of the search.
      function close() {
        setOpen(false);
        if (value.value !== "") {
          search.hidden = true;
          chosen.hidden = false;
        }
      }

      function choose(o) {
        value.value = o.dataset.value;
        summary.textContent = o.dataset.label;
        setOpen(false);
        search.hidden = true;
        chosen.hidden = false;
        value.dispatchEvent(new Event("change", { bubbles: true }));
        change.focus();
      }

      input.addEventListener("focus", function () { setOpen(true); });

      input.addEventListener("keydown", function (e) {
        const n = options().length;
        if (e.key === "ArrowDown" || e.key === "ArrowUp") {
          e.preventDefault();
          setOpen(true);
          if (n === 0) return;
          const step = e.key === "ArrowDown" ? 1 : -1;
          if (active < 0) setActive(step > 0 ? 0 : n - 1);
          else setActive((active + step + n) % n);
        } else if (e.key === "Enter") {
          // Enter picks the active row of an open list; otherwise it
          // submits the form as in any field (design-system §8.3).
          if (input.getAttribute("aria-expanded") === "true" && active >= 0) {
            e.preventDefault();
            choose(options()[active]);
          }
        } else if (e.key === "Escape") {
          e.preventDefault();
          close();
          if (search.hidden) change.focus();
        }
      });

      // New results: nothing is active until an arrow key moves.
      list.addEventListener("htmx:afterSwap", function () { if (recent) addRecent(); setActive(-1); });

      // Keep focus in the input while a row is clicked.
      list.addEventListener("mousedown", function (e) { e.preventDefault(); });
      list.addEventListener("click", function (e) {
        const o = e.target.closest("[role=option]");
        if (o && o.getAttribute("aria-disabled") !== "true") choose(o);
      });

      root.addEventListener("focusout", function (e) {
        if (!search.contains(e.relatedTarget)) close();
      });

      root.addEventListener("click", function (e) {
        if (e.target.closest("[data-picker-close]")) {
          close();
          if (search.hidden) change.focus();
          else root.focus();
        } else if (e.target.closest("[data-picker-change]")) {
          chosen.hidden = true;
          search.hidden = false;
          input.focus();
        } else if (e.target.closest("[data-picker-clear]")) {
          // The search text goes too: its input event makes htmx fetch the
          // rows for the empty text, so no old results stay under it.
          value.value = "";
          summary.textContent = "";
          input.value = "";
          chosen.hidden = true;
          search.hidden = false;
          input.dispatchEvent(new Event("input", { bubbles: true }));
          value.dispatchEvent(new Event("change", { bubbles: true }));
          root.focus();
        }
      });
    },

    destroy() {
      this.cleanup();
    },
  };
}

document.addEventListener("alpine:init", function () {
  window.Alpine.data("combobox", combobox);
});
