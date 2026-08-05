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

// Tapping the dark backdrop (not the modal box or anything inside it)
// closes whichever modal it belongs to. A click inside .modal-box always
// has some descendant element as e.target, never the overlay div itself,
// so this single check distinguishes "outside" from "inside" for every
// modal on the page without wiring up each one individually.
document.onclick = function (e) {
  e = e || window.event;
  var target = e.target || e.srcElement;
  if (target && (" " + target.className + " ").indexOf(" modal-overlay ") > -1) {
    target.style.display = "none";
  }
};

// Shelf toggle, instant, no page reload. Uses XMLHttpRequest rather than
// fetch() — fetch is almost certainly absent on this engine and throws
// synchronously before a .catch() can run (confirmed by an earlier version
// of this shelf-toggle interaction silently breaking on-device);
// XMLHttpRequest is a much older, more broadly supported API. isViewing is
// true only for the shelf currently being browsed (e.g. tapping "Favourites"
// while on the Favourites page) — since every book on that page is
// guaranteed already on that shelf, tapping it always means "remove," so on
// success the whole card and its modal are dropped from the page instead of
// just flipping the checkmark.
function toggleShelf(bookId, shelfId, btn, isViewing) {
  var xhr = new XMLHttpRequest();
  xhr.open("POST", "/books/" + bookId + "/shelves/" + shelfId, true);
  xhr.onreadystatechange = function () {
    if (xhr.readyState !== 4) {
      return;
    }
    if (xhr.status < 200 || xhr.status >= 400) {
      return;
    }
    if (isViewing) {
      var overlay = document.getElementById("book-" + bookId);
      var card = document.getElementById("card-" + bookId);
      if (overlay && overlay.parentNode) {
        overlay.parentNode.removeChild(overlay);
      }
      if (card && card.parentNode) {
        card.parentNode.removeChild(card);
      }
    } else if (btn) {
      if ((" " + btn.className + " ").indexOf(" is-on ") > -1) {
        btn.className = (" " + btn.className + " ").replace(" is-on ", " ").replace(/^\s+|\s+$/g, "");
      } else {
        btn.className += " is-on";
      }
    }
  };
  xhr.send();
  return false;
}
