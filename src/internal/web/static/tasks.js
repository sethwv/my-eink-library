(function () {
  var interval;
  var button;

  function start() {
    interval = window.setInterval(function () { window.location.reload(); }, 10000);
    button.textContent = "Pause refresh";
  }

  function stop() {
    window.clearInterval(interval);
    interval = 0;
    button.textContent = "Resume refresh";
  }

  window.addEventListener("DOMContentLoaded", function () {
    button = document.getElementById("task-poll-toggle");
    if (!button) { return; }
    start();
    button.addEventListener("click", function (event) {
      event.preventDefault();
      if (interval) {
        stop();
      } else {
        start();
      }
    });
  });
}());
