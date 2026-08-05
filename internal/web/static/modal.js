// Plain ES5, loaded on every page. Shows/hides modals via direct inline
// style, not CSS :target — :target proved unreliable on the actual target
// device (Kobo's QtWebKit) even though it's a very old, otherwise-safe CSS
// selector. No fetch, no arrow functions, no let/const: just
// getElementById and style.display, the most basic DOM API there is.
function openModal(id) {
  var el = document.getElementById(id);
  if (el) {
    el.style.display = "block";
  }
  return false;
}

function closeModal(id) {
  var el = document.getElementById(id);
  if (el) {
    el.style.display = "none";
  }
  return false;
}
