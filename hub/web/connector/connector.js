(function () {
  "use strict";

  var saved;
  try {
    saved = localStorage.getItem("maclawConnectorLang");
  } catch (_) {}

  var hashLangRe = /^#(zh|en|mn)-/;
  var hashMatch = window.location.hash.match(hashLangRe);
  var hashLang = hashMatch ? hashMatch[1] : "";
  var savedLang = saved === "zh" || saved === "en" || saved === "mn" ? saved : "";
  var lang = hashLang || savedLang || (navigator.language || "");
  var topButton = document.querySelector(".back-to-top");

  function setLang(next) {
    var raw = String(next || "").trim().toLowerCase().replace(/_/g, "-");
    if (raw === "zh" || raw.indexOf("zh-") === 0) lang = "zh";
    else if (raw === "mn-mong" || raw.indexOf("mn-mong-") === 0) lang = "en";
    else if (
      raw === "mn" ||
      raw === "mn-mn" ||
      raw === "mn-cyrl" ||
      raw === "mn-cyrl-mn" ||
      raw.indexOf("mn-cyrl-") === 0 ||
      raw.indexOf("mn-mn-") === 0
    ) lang = "mn";
    else lang = "en";
    var title = "MaClaw Third-Party Hardware Protocol";
    var subtitleText = "Third-party hardware protocol";
    var htmlLang = "en";
    if (lang === "zh") {
      title = "MaClaw 第三方硬件接入协议";
      subtitleText = "第三方硬件接入协议";
      htmlLang = "zh-CN";
    } else if (lang === "mn") {
      title = "MaClaw гуравдагч талын техник хангамжийн протокол";
      subtitleText = "Гуравдагч талын техник хангамжийн протокол";
      htmlLang = "mn";
    }
    document.documentElement.lang = htmlLang;
    document.title = title;
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
    if (subtitle) subtitle.textContent = subtitleText;
  }

  function setLangFromHash() {
    var match = window.location.hash.match(hashLangRe);
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
