// Form conveniences for the date form. Everything here is a prefill: the
// server never depends on it, and the operator can overwrite every value it
// writes. With scripting off the form still works — the booking code is simply
// typed by hand, as it was before.
(function () {
  "use strict";

  var course = document.querySelector('select[name="course_id"]');
  var start = document.getElementById("f-start");
  var end = document.getElementById("f-end");
  var code = document.getElementById("f-code");
  var language = document.getElementById("f-language");
  if (!course || !start || !end || !code || !language) return;

  // addDays does its arithmetic in UTC. A local-time Date would shift the day
  // across a DST boundary, which is exactly where a course week tends to sit.
  function addDays(iso, n) {
    var d = new Date(iso + "T00:00:00Z");
    if (isNaN(d.getTime())) return "";
    d.setUTCDate(d.getUTCDate() + n);
    return d.toISOString().slice(0, 10);
  }

  // Mirrors model.BookingCode. The two must agree, so keep them in step.
  function derivedCode() {
    var opt = course.options[course.selectedIndex];
    var token = opt ? opt.getAttribute("data-code-token") : "";
    if (!token || start.value.length !== 10) return "";
    return (
      start.value.slice(2, 4) + "-" + start.value.slice(5, 7) + " " + token +
      (language.value === "en" ? "-EN" : "")
    );
  }

  // A code that already differs from what we would derive was chosen on
  // purpose — "27-12 MSA" for a course that starts on 30 November — so leave
  // it alone. Anything the operator types later claims the field the same way.
  var manual = code.value !== "" && code.value !== derivedCode();
  code.addEventListener("input", function () {
    manual = true;
  });

  function refreshCode() {
    if (manual) return;
    var next = derivedCode();
    if (next) code.value = next;
  }

  // The last day can never precede the first, and a fresh course is far more
  // often two days than one, so the picker opens on start + 1 rather than on
  // whatever month it would otherwise default to.
  function refreshEnd() {
    if (!start.value) return;
    end.min = start.value;
    if (!end.value || end.value < start.value) {
      end.value = addDays(start.value, 1);
    }
  }

  start.addEventListener("change", function () {
    refreshEnd();
    refreshCode();
  });
  course.addEventListener("change", refreshCode);
  language.addEventListener("change", refreshCode);

  // On an existing date the bounds should hold from the first interaction, but
  // nothing already stored is rewritten just by opening the form.
  if (start.value) end.min = start.value;
})();
