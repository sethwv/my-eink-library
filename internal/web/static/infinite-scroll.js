// Plain ES5, progressive enhancement for the book-list pages (library,
// authors, series, favorites). Unlike modal.js's modal/shelf-toggle
// behavior, everything here is optional: the plain GET pagination links,
// jump-to-page form, and toolbar form (see library.html) all work
// perfectly without this file. #pagination-nav is only hidden once this
// script confirms #pagination-state exists, so a JS failure or an engine
// that never runs this file just leaves the working no-JS UI in place.
//
// No fetch() (throws synchronously pre-.catch() on the Kobo's QtWebKit
// engine, same reasoning as modal.js's toggleShelf), no
// URLSearchParams/querySelector/classList/getElementsByClassName (support
// on this engine is uncertain) - matches modal.js's getElementById +
// className-string conventions throughout.

var isLoading = false;
var hasNext = false;
var nextPage = null;
var loadMoreBtn = null;

function hasClass(el, cls) {
  return (" " + el.className + " ").indexOf(" " + cls + " ") > -1;
}

// Finds the first descendant of root with class cls, by walking every div
// (the only element .grid/.pagination-state ever are in this app's markup).
function findByClass(root, cls) {
  var divs = root.getElementsByTagName("div");
  for (var i = 0; i < divs.length; i++) {
    if (hasClass(divs[i], cls)) {
      return divs[i];
    }
  }
  return null;
}

function readPaginationState() {
  var el = document.getElementById("pagination-state");
  if (!el) {
    hasNext = false;
    nextPage = null;
    return;
  }
  hasNext = el.getAttribute("data-has-next") === "true";
  nextPage = parseInt(el.getAttribute("data-next-page"), 10);
}

// resetDir omits the dir param entirely (matching the no-JS <select
// onchange="this.form.submit()"> behavior, which never includes a dir
// field either) so the server applies defaultDirFor(newSort) instead of
// carrying over whatever direction the previous sort was using.
function currentFormState(resetDir) {
  var sortEl = document.getElementById("sort");
  var qEl = document.getElementById("q");
  var dirStateEl = document.getElementById("toolbar-dir-state");
  var nameEl = document.getElementById("toolbar-name");
  return {
    action: window.location.pathname,
    sort: sortEl ? sortEl.value : "title",
    dir: resetDir ? "" : (dirStateEl ? dirStateEl.value : "asc"),
    q: qEl ? qEl.value : "",
    name: nameEl ? nameEl.value : ""
  };
}

function buildQuery(state, page) {
  var parts = [];
  parts.push("sort=" + encodeURIComponent(state.sort));
  if (state.dir) {
    parts.push("dir=" + encodeURIComponent(state.dir));
  }
  parts.push("page=" + encodeURIComponent(page));
  if (state.q) {
    parts.push("q=" + encodeURIComponent(state.q));
  }
  if (state.name) {
    parts.push("name=" + encodeURIComponent(state.name));
  }
  return state.action + "?" + parts.join("&");
}

function xhrGet(url, onDone) {
  var xhr = new XMLHttpRequest();
  xhr.open("GET", url, true);
  xhr.setRequestHeader("X-Requested-With", "XMLHttpRequest");
  xhr.onreadystatechange = function () {
    if (xhr.readyState !== 4) {
      return;
    }
    if (xhr.status < 200 || xhr.status >= 400) {
      isLoading = false;
      return;
    }
    onDone(xhr.responseText);
  };
  xhr.send();
}

// Parses responseText (a "book_cards" + "pagination_state" fragment,
// server-rendered HTML from our own origin, same trust level as a normal
// page load) into a detached holder, then either appends the new cards to
// the live .grid (replace=false, "load more") or swaps the live .grid's
// contents entirely (replace=true, sort/search change). Always swaps in
// the fresh #pagination-state node so hasNext/nextPage stay current.
function applyResponse(html, replace) {
  var holder = document.createElement("div");
  holder.innerHTML = html;

  var liveGrid = findByClass(document.body, "grid");
  var newGrid = findByClass(holder, "grid");
  if (liveGrid && newGrid) {
    if (replace) {
      while (liveGrid.firstChild) {
        liveGrid.removeChild(liveGrid.firstChild);
      }
    }
    while (newGrid.firstChild) {
      liveGrid.appendChild(newGrid.firstChild);
    }
  }

  var oldState = document.getElementById("pagination-state");
  var newState = null;
  var divs = holder.getElementsByTagName("div");
  for (var i = 0; i < divs.length; i++) {
    if (divs[i].id === "pagination-state") {
      newState = divs[i];
      break;
    }
  }
  if (newState && oldState && oldState.parentNode) {
    oldState.parentNode.replaceChild(newState, oldState);
  }

  readPaginationState();
  updateLoadMoreVisibility();
  syncToolbarFromState();
}

// The server, not the client, decides the actual sort/dir/page used
// (e.g. an out-of-range page gets clamped, a sort-change resets dir via
// defaultDirFor) — sync the toolbar's <select>, hidden dir state, and
// dir-toggle button/label from the authoritative values the response
// carries in #pagination-state, rather than trusting what was requested.
function syncToolbarFromState() {
  var stateEl = document.getElementById("pagination-state");
  if (!stateEl) {
    return;
  }
  var sort = stateEl.getAttribute("data-sort");
  var dir = stateEl.getAttribute("data-dir");

  var sortEl = document.getElementById("sort");
  if (sortEl && sort) {
    sortEl.value = sort;
  }

  var dirStateEl = document.getElementById("toolbar-dir-state");
  if (dirStateEl && dir) {
    dirStateEl.value = dir;
  }

  var dirBtn = document.getElementById("dir-toggle-btn");
  if (dirBtn && dir) {
    dirBtn.value = dir === "asc" ? "desc" : "asc";
  }

  var ascLabel = document.getElementById("dir-asc-label");
  var descLabel = document.getElementById("dir-desc-label");
  if (ascLabel) {
    ascLabel.style.display = dir === "asc" ? "" : "none";
  }
  if (descLabel) {
    descLabel.style.display = dir === "desc" ? "" : "none";
  }
}

function loadMore() {
  if (isLoading || !hasNext || nextPage === null) {
    return;
  }
  isLoading = true;
  var state = currentFormState(false);
  xhrGet(buildQuery(state, nextPage), function (html) {
    applyResponse(html, false);
    isLoading = false;
  });
}

function doSortSearch(resetDir) {
  if (isLoading) {
    return false;
  }
  isLoading = true;
  var state = currentFormState(resetDir);
  var url = buildQuery(state, 1);
  xhrGet(url, function (html) {
    applyResponse(html, true);
    isLoading = false;
    window.scrollTo(0, 0);
    history.pushState(null, "", url);
  });
  return false;
}

function updateLoadMoreVisibility() {
  if (!loadMoreBtn) {
    return;
  }
  loadMoreBtn.style.display = hasNext ? "block" : "none";
}

// Flag-based throttle (setTimeout debounce) rather than a timer loop, to
// stay simple/ES5. Triggers a load-more once within ~800px of the bottom.
var scrollScheduled = false;

function onScrollOrResize() {
  if (scrollScheduled) {
    return;
  }
  scrollScheduled = true;
  setTimeout(function () {
    scrollScheduled = false;
    var scrollY = window.pageYOffset || document.documentElement.scrollTop || 0;
    var viewport = window.innerHeight || document.documentElement.clientHeight || 0;
    var full = document.body.scrollHeight || 0;
    if (full - (scrollY + viewport) < 800) {
      loadMore();
    }
  }, 150);
}

function initInfiniteScroll() {
  if (!document.getElementById("pagination-state")) {
    // Not a book-list page (or a template render error) - never touch the
    // no-JS pagination fallback if there's nothing to enhance.
    return;
  }
  readPaginationState();

  var nav = document.getElementById("pagination-nav");
  if (nav) {
    nav.style.display = "none";
  }

  var liveGrid = findByClass(document.body, "grid");
  if (liveGrid && liveGrid.parentNode) {
    loadMoreBtn = document.createElement("button");
    loadMoreBtn.type = "button";
    loadMoreBtn.className = "load-more-btn";
    loadMoreBtn.appendChild(document.createTextNode("Load more"));
    loadMoreBtn.onclick = loadMore;
    liveGrid.parentNode.insertBefore(loadMoreBtn, liveGrid.nextSibling);
    updateLoadMoreVisibility();
  }

  window.onscroll = onScrollOrResize;
  window.onresize = onScrollOrResize;

  var sortEl = document.getElementById("sort");
  if (sortEl) {
    sortEl.onchange = function () {
      return doSortSearch(true);
    };
  }

  var toolbarForm = document.getElementById("toolbar-form");
  if (toolbarForm) {
    toolbarForm.onsubmit = function () {
      return doSortSearch(false);
    };
  }

  var dirBtn = document.getElementById("dir-toggle-btn");
  if (dirBtn) {
    dirBtn.onclick = function () {
      // dirBtn.value holds the toggle target (the *new* direction, set
      // server-side as .ToggleDir) - write it into the hidden state field
      // so this request asks for it; syncToolbarFromState() updates the
      // button/labels to match once the (authoritative) response lands.
      var dirStateEl = document.getElementById("toolbar-dir-state");
      if (dirStateEl) {
        dirStateEl.value = dirBtn.value;
      }
      return doSortSearch(false);
    };
  }
}

// Chain onto any window.onload already set (modal.js sets one for its
// ?book= direct-link handling) rather than overwrite it.
var previousOnload = window.onload;
window.onload = function () {
  if (previousOnload) {
    previousOnload();
  }
  initInfiniteScroll();
};

window.onpopstate = function () {
  location.reload();
};
