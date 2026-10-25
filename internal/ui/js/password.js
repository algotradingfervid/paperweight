// password.js: the "Show password" / "Hide password" toggle of PasswordInput.
document.addEventListener("click", function (e) {
  const btn = e.target.closest("[data-toggle-password]");
  if (!btn) return;
  const input = document.getElementById(btn.getAttribute("aria-controls"));
  if (!input) return;
  const show = input.type === "password";
  input.type = show ? "text" : "password";
  btn.textContent = show ? "Hide password" : "Show password";
});
