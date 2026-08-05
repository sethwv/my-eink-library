// Progressive enhancement for shelf-toggle forms inside the "+" modal:
// submit via fetch so toggling a shelf doesn't reload the page/close the
// modal. Falls back to a real form submit (full page redirect) if the
// fetch fails, so the feature still works with JS disabled.
document.addEventListener("submit", function (event) {
  var form = event.target;
  if (!form.classList.contains("shelf-toggle-form")) return;

  event.preventDefault();
  var button = form.querySelector("button[type=submit]");

  fetch(form.action, { method: "POST", body: new FormData(form) })
    .then(function (res) {
      if (!res.ok) throw new Error("shelf toggle failed");
      if (button) button.classList.toggle("is-on");
    })
    .catch(function () {
      form.submit();
    });
});
