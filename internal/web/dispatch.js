// dispatch.js — the Agent-K inbox: expand a ticket's inline detail, and the score-info tooltip.
(function () {
  "use strict";

  // Expand / collapse a ticket's inline detail when its "Review" control is tapped. The title stays a plain link to
  // the full /r/{id} page, so this only fires for elements marked data-act="review".
  document.addEventListener("click", function (e) {
    var act = e.target.closest('[data-act="review"]');
    if (!act) return;
    var card = act.closest(".ticket");
    if (!card) return;
    e.preventDefault();
    var open = card.classList.toggle("open");
    var detail = card.querySelector(".detail");
    if (detail) detail.hidden = !open;
    var row = card.querySelector(".tkrow");
    if (row) row.setAttribute("aria-expanded", open ? "true" : "false");
  });

  // Score tooltip: hover on desktop (CSS), tap to pin on touch.
  var wrap = document.getElementById("scoreinfo");
  if (wrap) {
    var btn = document.getElementById("scoreinfo-btn");
    var close = function () {
      wrap.classList.remove("open");
      if (btn) btn.setAttribute("aria-expanded", "false");
    };
    if (btn) {
      btn.addEventListener("click", function (e) {
        e.stopPropagation();
        var o = wrap.classList.toggle("open");
        btn.setAttribute("aria-expanded", o ? "true" : "false");
      });
    }
    document.addEventListener("click", function (e) {
      if (!wrap.contains(e.target)) close();
    });
    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape") close();
    });
  }
})();
