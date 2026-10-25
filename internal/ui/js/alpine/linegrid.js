// linegrid.js: keyboard entry for the desktop line grid (design-system §8.2).
// Enter moves to the next row's first input; Ctrl+Enter clicks the
// [data-add-line] control ("Add an item").

document.addEventListener("alpine:init", function () {
  window.Alpine.data("linegrid", function () {
    return {
      init: function () {
        this.$el.addEventListener("keydown", this.onKey.bind(this));
      },
      onKey: function (evt) {
        if (evt.key !== "Enter" || evt.target.tagName !== "INPUT") return;
        evt.preventDefault();
        if (evt.ctrlKey) {
          const add = document.querySelector("[data-add-line]");
          if (add) add.click();
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
