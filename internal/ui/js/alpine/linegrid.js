// linegrid.js: keyboard entry for the desktop line grid (design-system §8.2).
// Enter moves to the next row's first input; Ctrl+Enter clicks the
// [data-add-line] control ("Add an item"), after saving an unsaved edit.

document.addEventListener("alpine:init", function () {
  window.Alpine.data("linegrid", function () {
    return {
      // stop removes the listeners of a pending Ctrl+Enter save, if any.
      stop: null,
      init: function () {
        this.$el.addEventListener("keydown", this.onKey.bind(this));
      },
      onKey: function (evt) {
        if (this.stop) this.stop();
        if (evt.key !== "Enter" || evt.target.tagName !== "INPUT") return;
        if (evt.target.getAttribute("role") === "combobox") return;
        evt.preventDefault();
        if (evt.ctrlKey) {
          const add = document.querySelector("[data-add-line]");
          const input = evt.target;
          const form = input.form;
          if (!add) return;
          if (!form || input.value === input.defaultValue) {
            add.click();
            return;
          }
          // Blurring fires change, which saves the row; add only once the
          // save succeeded. A refused (422) or conflicted (409) save is
          // swapped in too, so stay on it and focus its first invalid input.
          const self = this;
          const id = form.id;
          const done = function (e) {
            if (e.target !== form) return;
            self.stop();
            const x = e.detail.xhr;
            if (e.detail.successful && x && x.status >= 200 && x.status < 300) {
              add.click();
              return;
            }
            const row = document.getElementById(id);
            const bad = row && row.querySelector("[aria-invalid=true]");
            if (bad) bad.focus();
          };
          const halted = function (e) {
            if (e.target === form) self.stop();
          };
          this.stop = function () {
            form.removeEventListener("htmx:afterRequest", done);
            form.removeEventListener("htmx:validation:halted", halted);
            self.stop = null;
          };
          form.addEventListener("htmx:afterRequest", done);
          form.addEventListener("htmx:validation:halted", halted);
          input.blur();
          return;
        }
        const rows = Array.from(this.$el.querySelectorAll('form[id^="grid-row-"]'));
        const next = rows[rows.indexOf(evt.target.form) + 1];
        const input = next && next.querySelector("input:not([type=hidden])");
        if (input) input.focus();
      },
    };
  });
});
