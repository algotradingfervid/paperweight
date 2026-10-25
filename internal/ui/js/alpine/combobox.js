// combobox.js: the Picker's ARIA 1.2 combobox (x-data="combobox"). htmx
// fills the listbox; this moves aria-activedescendant with the arrow keys,
// writes the chosen id and summary on Enter or click, and closes on Esc.
// Closing after "Change" without a new choice shows the old summary again.
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
      let active = -1;

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

      function setOpen(open) {
        input.setAttribute("aria-expanded", open ? "true" : "false");
        search.toggleAttribute("data-open", open);
        if (!open) setActive(-1);
      }

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
      list.addEventListener("htmx:afterSwap", function () { setActive(-1); });

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
        } else if (e.target.closest("[data-picker-change]")) {
          chosen.hidden = true;
          search.hidden = false;
          input.focus();
        }
      });
    },
  };
}

document.addEventListener("alpine:init", function () {
  window.Alpine.data("combobox", combobox);
});
