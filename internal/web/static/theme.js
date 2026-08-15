// Appearance preferences must use cookies rather than localStorage because
// Kobo's QtWebKit browser does not provide Web Storage. Keep this ES5-only,
// like modal.js, so it can run on the target device.
(function () {
  var cookieName = "eink-library-theme";

  function preference() {
    var cookies = document.cookie ? document.cookie.split(";") : [];
    for (var i = 0; i < cookies.length; i++) {
      var cookie = cookies[i].replace(/^\s+/, "");
      if (cookie.indexOf(cookieName + "=") === 0) {
        var value = cookie.substring(cookieName.length + 1);
        if (value === "light" || value === "dark" || value === "auto") {
          return value;
        }
      }
    }
    return "auto";
  }

  function resolvedTheme(mode) {
    if (mode === "dark") {
      return "dark";
    }
    if (mode === "auto" && window.matchMedia) {
      try {
        if (window.matchMedia("(prefers-color-scheme: dark)").matches) {
          return "dark";
        }
      } catch (err) {
        // Older engines may not parse the color-scheme media feature.
      }
    }
    return "light";
  }

  function applyTheme(mode) {
    var root = document.documentElement;
    root.className = root.className.replace(/\btheme-(light|dark)\b/g, "").replace(/^\s+|\s+$/g, "");
    root.className += (root.className ? " " : "") + "theme-" + resolvedTheme(mode);
  }

  window.setThemeMode = function (mode) {
    if (mode !== "light" && mode !== "dark" && mode !== "auto") {
      mode = "auto";
    }
    var expires = new Date();
    expires.setFullYear(expires.getFullYear() + 1);
    document.cookie = cookieName + "=" + mode + "; expires=" + expires.toUTCString() + "; path=/";
    applyTheme(mode);
    window.syncThemeModeControl();
  };

  window.syncThemeModeControl = function () {
    var control = document.getElementById("theme-mode");
    if (control) {
      control.value = preference();
    }
  };

  applyTheme(preference());
}());
