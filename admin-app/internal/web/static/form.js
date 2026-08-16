// Progressive enhancement for the date and course forms. Nothing here is
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

  function addDays(iso, n) {
    // UTC arithmetic: a local-time Date shifts the day across a DST boundary,
    // which is exactly where a course week tends to sit.
    var d = new Date(iso + "T00:00:00Z");
    if (isNaN(d.getTime())) return "";
    d.setUTCDate(d.getUTCDate() + n);
    return d.toISOString().slice(0, 10);
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

    // Mirrors model.BookingCode. The two must agree — keep them in step.
    var derivedCode = function () {
      var token = attr("data-code-token");
      if (!token || start.value.length !== 10) return "";
      return start.value.slice(2, 4) + "-" + start.value.slice(5, 7) + " " + token +
        (language.value === "en" ? "-EN" : "");
    };
    // Mirrors model.DateID.
    var derivedID = function () {
      if (!course.value || start.value.length !== 10) return "";
      var m = parseInt(start.value.slice(5, 7), 10);
      if (!(m >= 1 && m <= 12)) return "";
      return course.value + "-" + MONTHS[m - 1] + "-" + start.value.slice(0, 4);
    };

    var codeState = claim(code, derivedCode);
    // The id is only ever derived while creating: a published id is the anchor
    // people have bookmarked, so on an edit form it is left strictly alone.
    var isNew = document.querySelector('form.detail').getAttribute("action").indexOf("/dates/new") === 0;
    var idState = isNew ? claim(idField, derivedID) : null;

    var city = document.querySelector('input[name="city"]');
    var country = document.querySelector('input[name="country"]');
    var pricing = document.querySelector('input[name="pricing"]');
    var cityState = claim(city, function () { return attr("data-city"); });
    var countryState = claim(country, function () { return attr("data-country"); });
    var pricingState = claim(pricing, function () { return attr("data-pricing"); });

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
    language.addEventListener("change", function () { codeState.apply(); });
    course.addEventListener("change", function () {
      codeState.apply();
      if (idState) idState.apply();
      cityState.apply();
      countryState.apply();
      pricingState.apply();
    });

    if (start.value) end.min = start.value;
  }

  /* ------------------------------------------------------- course form -- */

  var cid = document.getElementById("c-id");
  var curl = document.getElementById("c-url");
  if (cid && curl) {
    var derivedURL = function () {
      return cid.value ? "https://www.arc42.de/info-" + cid.value + "/" : "";
    };
    var urlState = claim(curl, derivedURL);
    cid.addEventListener("input", function () { urlState.apply(); });
  }
})();
