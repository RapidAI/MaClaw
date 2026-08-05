(function () {
  "use strict";

  var saved;
  try {
    saved = localStorage.getItem("maclawConnectorLang");
  } catch (_) {}

  var hashMatch = window.location.hash.match(/^#(zh|en)-/);
  var hashLang = hashMatch ? hashMatch[1] : "";
  var lang = hashLang || saved || ((navigator.language || "").toLowerCase().startsWith("zh") ? "zh" : "en");
  var topButton = document.querySelector(".back-to-top");

  function setLang(next) {
    lang = next === "en" ? "en" : "zh";
    document.documentElement.lang = lang === "zh" ? "zh-CN" : "en";
    document.title = lang === "zh" ? "MaClaw 第三方硬件接入协议" : "MaClaw Third-Party Hardware Protocol";
    try {
      localStorage.setItem("maclawConnectorLang", lang);
    } catch (_) {}
    document.querySelectorAll("[data-lang-panel]").forEach(function (element) {
      element.classList.toggle("active", element.getAttribute("data-lang-panel") === lang);
    });
    document.querySelectorAll("[data-lang]").forEach(function (button) {
      var active = button.getAttribute("data-lang") === lang;
      button.classList.toggle("active", active);
      button.setAttribute("aria-pressed", active ? "true" : "false");
    });
    var subtitle = document.querySelector("[data-i18n=headerSubtitle]");
    if (subtitle) {
      subtitle.textContent = lang === "zh" ? "第三方硬件接入协议" : "Third-party hardware protocol";
    }
  }

  function setLangFromHash() {
    var match = window.location.hash.match(/^#(zh|en)-/);
    if (match && match[1] !== lang) setLang(match[1]);
  }

  document.querySelectorAll("[data-lang]").forEach(function (button) {
    button.addEventListener("click", function () {
      setLang(button.getAttribute("data-lang"));
    });
  });

  function updateTopButton() {
    if (topButton) topButton.classList.toggle("visible", window.scrollY > 640);
  }

  if (topButton) {
    topButton.addEventListener("click", function () {
      var reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      window.scrollTo({ top: 0, behavior: reducedMotion ? "auto" : "smooth" });
    });
    window.addEventListener("scroll", updateTopButton, { passive: true });
  }

  window.addEventListener("hashchange", setLangFromHash);

  setLang(lang);
  updateTopButton();
})();
