// Progressive enhancement for shelf-toggle forms inside the "+" modal:
// submit via fetch so toggling a shelf doesn't reload the page/close the
// modal. Falls back to a real form submit (full page redirect) if the
// fetch fails, so the feature still works with JS disabled.
document.addEventListener("submit", function (event) {
  var form = event.target;
  if (!form.classList.contains("shelf-toggle-form")) return;

  event.preventDefault();
  var button = form.querySelector("button[type=submit]");
  var modal = form.closest(".modal-overlay");

  fetch(form.action, { method: "POST", body: new FormData(form) })
    .then(function (res) {
      if (!res.ok) throw new Error("shelf toggle failed");
      if (button) button.classList.toggle("is-on");
      if (modal) updatePlusButton(modal);
    })
    .catch(function () {
      form.submit();
    });
});

// Reflects whether the book is on any shelf onto its "+" button, which
// lives outside the modal (in .cover-wrap) and links to it by id.
function updatePlusButton(modal) {
  var onAny = !!modal.querySelector(".shelf-toggle.is-on");
  var plusButton = document.querySelector('a[href="#' + modal.id + '"]');
  if (plusButton) plusButton.classList.toggle("is-on", onAny);
}
