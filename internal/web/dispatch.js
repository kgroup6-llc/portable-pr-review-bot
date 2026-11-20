// dispatch.js — the Agent-K inbox: inline detail toggle, score tooltip, and swipe mode.
(function () {
  "use strict";

  // ---- inline detail toggle --------------------------------------------------
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

  // ---- score tooltip ---------------------------------------------------------
  (function () {
    var wrap = document.getElementById("scoreinfo");
    if (!wrap) return;
    var btn = document.getElementById("scoreinfo-btn");
    var close = function () { wrap.classList.remove("open"); if (btn) btn.setAttribute("aria-expanded", "false"); };
    if (btn) btn.addEventListener("click", function (e) {
      e.stopPropagation();
      var o = wrap.classList.toggle("open");
      btn.setAttribute("aria-expanded", o ? "true" : "false");
    });
    document.addEventListener("click", function (e) { if (!wrap.contains(e.target)) close(); });
    document.addEventListener("keydown", function (e) { if (e.key === "Escape") close(); });
  })();

  // ---- view mode: scroll (default) | swipe ----------------------------------
  var dispatch = document.querySelector(".dispatch");
  if (!dispatch) return;
  var KEY = "prb-view";
  function load() { try { return localStorage.getItem(KEY) === "swipe" ? "swipe" : "scroll"; } catch (e) { return "scroll"; } }
  function save(v) { try { localStorage.setItem(KEY, v); } catch (e) {} }

  var btnScroll = document.getElementById("m-scroll"), btnSwipe = document.getElementById("m-swipe");

  // restack marks the first ticket in a tray as the top of the stack and updates its counter.
  function restack(tray) {
    var cards = Array.prototype.slice.call(tray.querySelectorAll(".ticket"));
    tray.classList.toggle("multi", cards.length > 1);
    cards.forEach(function (c, i) {
      c.classList.toggle("top", i === 0);
      c.style.transform = "";
      c.style.opacity = "";
    });
    var group = tray.closest(".group");
    var count = group && group.querySelector(".swcount");
    if (count) count.textContent = cards.length ? "1 of " + cards.length : "all clear";
    var later = group && group.querySelector(".swlater");
    if (later) later.disabled = cards.length < 2;
  }

  function setView(v) {
    v = v === "swipe" ? "swipe" : "scroll";
    dispatch.setAttribute("data-view", v);
    save(v);
    if (btnScroll) btnScroll.setAttribute("aria-pressed", v === "scroll");
    if (btnSwipe) btnSwipe.setAttribute("aria-pressed", v === "swipe");
    dispatch.querySelectorAll(".tray").forEach(restack);
  }
  if (btnScroll) btnScroll.addEventListener("click", function () { setView("scroll"); });
  if (btnSwipe) btnSwipe.addEventListener("click", function () { setView("swipe"); });

  // later sends the top card to the back of its stack (no server action — just triage order).
  function later(tray) {
    var top = tray.querySelector(".ticket.top");
    if (top) tray.appendChild(top);
    restack(tray);
  }
  dispatch.querySelectorAll(".swlater").forEach(function (b) {
    b.addEventListener("click", function () {
      var group = b.closest(".group");
      if (group) later(group.querySelector(".tray"));
    });
  });

  // drag the top card: right past the threshold fires its primary action, left sends it to the back.
  var drag = null;
  var THRESH = 110;
  document.addEventListener("pointerdown", function (e) {
    if (dispatch.getAttribute("data-view") !== "swipe") return;
    var card = e.target.closest(".ticket.top");
    if (!card || !dispatch.contains(card)) return;
    if (e.target.closest("button,a,input,label,textarea,select")) return; // let the card's own controls work
    drag = { card: card, x0: e.clientX, dx: 0 };
    card.classList.add("drag");
    if (card.setPointerCapture) card.setPointerCapture(e.pointerId);
  });
  document.addEventListener("pointermove", function (e) {
    if (!drag) return;
    drag.dx = e.clientX - drag.x0;
    drag.card.style.transform = "translateX(" + drag.dx + "px) rotate(" + (drag.dx / 60) + "deg)";
    drag.card.style.opacity = String(1 - Math.min(Math.abs(drag.dx) / 600, 0.35));
  });
  document.addEventListener("pointerup", function () {
    if (!drag) return;
    var card = drag.card, dx = drag.dx, tray = card.closest(".tray");
    card.classList.remove("drag");
    if (dx > THRESH) {                 // swipe right → the card's primary action (Approve, or open the review)
      var hero = card.querySelector(".btn.hero");
      card.style.transform = "translateX(120%) rotate(8deg)";
      card.style.opacity = "0";
      if (hero) setTimeout(function () { hero.click(); }, 180);
    } else if (dx < -THRESH) {         // swipe left → later
      card.style.transform = "translateX(-120%) rotate(-8deg)";
      card.style.opacity = "0";
      setTimeout(function () { later(tray); }, 180);
    } else {                           // snap back
      card.style.transform = "";
      card.style.opacity = "";
    }
    drag = null;
  });

  setView(load());
})();
