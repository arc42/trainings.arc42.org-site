// Progressive enhancement for the date, course and propose forms. Nothing here is
// load-bearing: every value it fills in is editable, every check it runs is
// repeated on the server, and with scripting off the hints are simply visible
// all the time instead of behind a "?" button.
(function () {
  "use strict";

  /* ---------------------------------------------------------------- help --
     Each hint is server-rendered next to its control. Here they collapse
     behind a "?" that sits at the end of the label text, so an expert sees a
     clean form and a newcomer is one click from the rule. */

  function attachHelp(hint) {
    var host = hint.closest("label") || hint.closest("fieldset");
    if (!host) return;

    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = "help";
    btn.textContent = "?";
    btn.setAttribute("aria-controls", hint.id);
    btn.setAttribute("aria-expanded", "false");
    // The accessible name has to say what it explains: a form of twelve
    // buttons all called "?" is useless to anyone not looking at it.
    var label = (host.textContent || "").trim().split("\n")[0].trim();
    btn.setAttribute("aria-label", "Explain " + (label || "this field"));

    hint.hidden = true;
    btn.addEventListener("click", function () {
      hint.hidden = !hint.hidden;
      btn.setAttribute("aria-expanded", String(!hint.hidden));
    });

    // Sit the button just before the control it describes, which puts it at
    // the end of the label text where a reader expects it.
    var control = host.querySelector("input, select, textarea");
    if (control && control.parentNode === host) host.insertBefore(btn, control);
    else host.insertBefore(btn, hint);
  }

  Array.prototype.forEach.call(document.querySelectorAll(".hint[id]"), attachHelp);

  /* ------------------------------------------------------- lower case --
     Ids are lower case. A field marked data-lowercase shows that as it is
     typed rather than complaining at the save: "MAISPEC" becomes "maispec"
     under the operator's fingers, and once the field is left, "Mai Spec"
     becomes "mai-spec", exactly what the server will store. The server
     normalises the same way, so with scripting off nothing is lost but this
     preview. Registered before the course form's own listeners. */

  // Mirrors normaliseID in checks.go: trim, lower-case, each run of spaces
  // and underscores one hyphen. \s is spelled out because Go's \s is ASCII
  // only. TestFormJSNormalisesLikeTheServer keeps the two in step.
  function normaliseId(v) {
    return v.trim().toLowerCase().replace(/[ \t\n\f\r_]+/g, "-");
  }

  function lowerAsTyped(el) {
    el.addEventListener("input", function () {
      var typed = el.value;
      var lower = typed.toLowerCase();
      if (typed === lower) return;
      // Keep the caret where it was. A few characters lower-case to a
      // different length (İ); then it moves by the difference, which still
      // leaves it next to what was just typed.
      var shift = lower.length - typed.length;
      var start = el.selectionStart, end = el.selectionEnd;
      el.value = lower;
      if (start === null || end === null) return;
      try {
        el.setSelectionRange(Math.max(0, start + shift), Math.max(0, end + shift));
      } catch (e) { /* not a text control; the value is right regardless */ }
    });
    // Spaces become hyphens only on leaving the field: converting them while
    // typing would turn "mai " into "mai-" before the next word is there.
    el.addEventListener("change", function () {
      var id = normaliseId(el.value);
      if (id !== el.value) el.value = id;
    });
  }

  Array.prototype.forEach.call(document.querySelectorAll("input[data-lowercase]"), lowerAsTyped);

  /* ------------------------------------------------------------ helpers -- */

  // A field is "ours" to fill in until the operator types in it. Anything they
  // wrote by hand — or that was already stored and differs from what we would
  // derive — is theirs, and we never overwrite it.
  function claim(el, derived) {
    if (!el) return null;
    var state = { manual: el.value !== "" && el.value !== derived() };
    el.addEventListener("input", function () { state.manual = true; });
    state.apply = function () {
      if (state.manual) return;
      var next = derived();
      if (next) el.value = next;
    };
    return state;
  }

  // Placeholders say "e.g." so that no example can be read as a stored value.
  // An empty suggestion clears the placeholder rather than showing a bare
  // "e.g.": for a course with no dates yet there is nothing to suggest.
  function example(el, value) {
    if (!el) return;
    el.placeholder = value ? "e.g. " + value : "";
  }

  function addDays(iso, n) {
    // UTC arithmetic: a local-time Date shifts the day across a DST boundary,
    // which is exactly where a course week tends to sit.
    var d = new Date(iso + "T00:00:00Z");
    if (isNaN(d.getTime())) return "";
    d.setUTCDate(d.getUTCDate() + n);
    return d.toISOString().slice(0, 10);
  }

  // A computed field is shown read-only while it is ours, so it reads as a
  // result rather than a question. "Override" hands it to the operator for
  // the rare real exception; a stored value that already differs from the
  // derivation is theirs from the start and never locked.
  function lock(el, state, what) {
    if (!el || !state) return;
    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = "link override";
    btn.textContent = "Override";
    btn.setAttribute("aria-label", "Override the computed " + what);
    var sync = function () {
      el.readOnly = !state.manual;
      btn.hidden = state.manual;
    };
    btn.addEventListener("click", function () {
      state.manual = true;
      sync();
      el.focus();
      el.select();
    });
    el.insertAdjacentElement("afterend", btn);
    sync();
  }

  var MONTHS = ["jan", "feb", "mar", "apr", "may", "jun",
                "jul", "aug", "sep", "oct", "nov", "dec"];

  /* --------------------------------------------------------- date form -- */

  var course = document.querySelector('select[name="course_id"]');
  var start = document.getElementById("f-start");
  var end = document.getElementById("f-end");
  var code = document.getElementById("f-code");
  var language = document.getElementById("f-language");
  var idField = document.querySelector('input[name="id"]');

  if (course && start && end && code && language && idField) {
    var opt = function () { return course.options[course.selectedIndex]; };
    var attr = function (name) {
      var o = opt();
      return o ? o.getAttribute(name) || "" : "";
    };

    // What the other dates already use, rendered by the server (takenJSON).
    var takenOf = function (el) {
      try { return JSON.parse(el.getAttribute("data-taken") || "[]"); }
      catch (e) { return []; }
    };
    var takenCodes = takenOf(code), takenIDs = takenOf(idField);
    // Mirrors model.NextFree: a second run in the same month and language
    // gets "-2", so the form never fills in a value another date has.
    var nextFree = function (base, taken) {
      if (!base || taken.indexOf(base) < 0) return base;
      for (var n = 2; ; n++) {
        if (taken.indexOf(base + "-" + n) < 0) return base + "-" + n;
      }
    };

    // Mirrors model.Taken.BookingCode. The two must agree — keep them in step.
    var derivedCode = function () {
      var token = attr("data-code-token");
      if (!token || start.value.length !== 10) return "";
      return nextFree(start.value.slice(2, 4) + "-" + start.value.slice(5, 7) + " " + token +
        (language.value === "en" ? "-EN" : ""), takenCodes);
    };
    // Mirrors model.Taken.DateID.
    var derivedID = function () {
      if (!course.value || start.value.length !== 10) return "";
      var m = parseInt(start.value.slice(5, 7), 10);
      if (!(m >= 1 && m <= 12)) return "";
      return nextFree(course.value + "-" + MONTHS[m - 1] + "-" + start.value.slice(0, 4) +
        (language.value === "en" ? "-en" : ""), takenIDs);
    };

    // Mirrors model.IDMask / model.CodeMask. The server renders these for the
    // first paint; here they follow the dropdown, because a date id reading
    // "msa-..." while the selected course is Req4Arc is worse than no hint at
    // all. Masks, not examples: both fields fill themselves in the moment a
    // first day is set, so what is useful in the gap before that is the shape.
    var refreshMasks = function () {
      idField.placeholder = (course.value || "course") + "-mmm-yyyy";
      code.placeholder = "YY-MM " + (attr("data-code-token") || "CODE");
      // The city and country defaults are already per-course; the placeholders
      // had been frozen at Munich and DE.
      example(city, attr("data-city"));
      example(country, attr("data-country"));
    };

    var codeState = claim(code, derivedCode);
    // The id is only ever derived while creating: a published id is the anchor
    // people have bookmarked, so on an edit form it is left strictly alone.
    var isNew = document.querySelector('form.detail').getAttribute("action").indexOf("/dates/new") === 0;
    var idState = isNew ? claim(idField, derivedID) : null;
    lock(code, codeState, "booking code");
    lock(idField, idState, "date id");

    var city = document.querySelector('input[name="city"]');
    var country = document.querySelector('input[name="country"]');
    var priceAmount = document.querySelector('input[name="price_amount"]');
    var cityState = claim(city, function () { return attr("data-city"); });
    var countryState = claim(country, function () { return attr("data-country"); });
    var priceState = claim(priceAmount, function () { return attr("data-price-amount"); });

    var refreshEnd = function () {
      if (!start.value) return;
      end.min = start.value;
      // The last day can never precede the first, and a course is far more
      // often two days than one, so the picker opens on start + 1.
      if (!end.value || end.value < start.value) end.value = addDays(start.value, 1);
    };

    start.addEventListener("change", function () {
      refreshEnd();
      codeState.apply();
      if (idState) idState.apply();
    });
    language.addEventListener("change", function () {
      codeState.apply();
      if (idState) idState.apply();
    });
    course.addEventListener("change", function () {
      codeState.apply();
      if (idState) idState.apply();
      cityState.apply();
      countryState.apply();
      priceState.apply();
      refreshMasks();
    });

    // On a blank new-date form the server has no course to render masks for,
    // while the browser has already selected the first option. Run once so the
    // two agree before anything is touched.
    refreshMasks();

    if (start.value) end.min = start.value;
  }

  /* --------------------------------------------------------- submit once --
     A form marked data-submit-once disables its button the moment it is
     sent: opening a pull request takes a few seconds of GitHub round trips,
     long enough for a second click. The server serialises submits per session
     and would answer a second one with the same PR anyway; this just stops
     the second request from being sent. The submit event only fires once the
     browser's own validation passed, so an invalid form stays clickable. */

  Array.prototype.forEach.call(document.querySelectorAll("form[data-submit-once]"), function (form) {
    form.addEventListener("submit", function () {
      Array.prototype.forEach.call(form.querySelectorAll('button[type="submit"]'), function (btn) {
        btn.disabled = true;
        if (btn.dataset.busy) btn.textContent = btn.dataset.busy;
      });
    });
  });
  // Back from the result page restores this page from the bfcache with the
  // button still disabled; give it back.
  window.addEventListener("pageshow", function (e) {
    if (!e.persisted) return;
    Array.prototype.forEach.call(document.querySelectorAll("form[data-submit-once] button[disabled]"), function (btn) {
      btn.disabled = false;
      if (btn.dataset.idle) btn.textContent = btn.dataset.idle;
    });
  });

  /* ------------------------------------------------------- course form -- */

  var cid = document.getElementById("c-id");
  var curl = document.getElementById("c-url");
  if (cid && curl) {
    // From the id the server will store, not the one in the field: with
    // "Mai Spec" typed, the page is info-mai-spec/, never info-mai spec/.
    // Mirrors followID in handlers_courses.go.
    var derivedURL = function () {
      var id = normaliseId(cid.value);
      return id ? "https://www.arc42.de/info-" + id + "/" : "";
    };
    var urlState = claim(curl, derivedURL);
    cid.addEventListener("input", function () { urlState.apply(); });
    cid.addEventListener("change", function () { urlState.apply(); });
  }
})();
