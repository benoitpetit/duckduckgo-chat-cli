const form = document.querySelector("#loginForm");
const password = document.querySelector("#dashboardPassword");
const errorMessage = document.querySelector("#loginError");
const mascot = document.querySelector("#loginMascot");

if (mascot && !window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
  const resetEyes = () => {
    mascot.style.setProperty("--pupil-x", "0px");
    mascot.style.setProperty("--pupil-y", "0px");
  };

  window.addEventListener("pointermove", event => {
    const bounds = mascot.getBoundingClientRect();
    const eyeCenterX = bounds.left + bounds.width / 2;
    const eyeCenterY = bounds.top + bounds.height * 0.42;
    const x = Math.max(-10, Math.min(10, ((event.clientX - eyeCenterX) / (window.innerWidth / 2)) * 10));
    const y = Math.max(-8, Math.min(8, ((event.clientY - eyeCenterY) / (window.innerHeight / 2)) * 8));
    mascot.style.setProperty("--pupil-x", `${x}px`);
    mascot.style.setProperty("--pupil-y", `${y}px`);
  }, { passive: true });
  window.addEventListener("blur", resetEyes);
  document.documentElement.addEventListener("pointerleave", resetEyes);
}

password.addEventListener("focus", () => mascot?.classList.add("password-focused"));
password.addEventListener("blur", () => mascot?.classList.remove("password-focused"));

form.addEventListener("submit", async event => {
  event.preventDefault();
  errorMessage.hidden = true;
  const submit = form.querySelector("button[type=submit]");
  submit.disabled = true;
  try {
    const response = await fetch("/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ password: password.value }),
      cache: "no-store",
    });
    if (!response.ok) throw new Error("The password could not be verified. Try again.");
    window.location.assign("/");
  } catch (error) {
    errorMessage.textContent = error.message || "Sign-in failed. Try again.";
    errorMessage.hidden = false;
    password.select();
  } finally {
    submit.disabled = false;
  }
});
