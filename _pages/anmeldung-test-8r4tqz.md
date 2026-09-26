---
title: "Anmeldung (Test)"
layout: page
permalink: /anmeldung-test-8r4tqz/
lang: de
locale: de_DE
translation_url: /registration-test-8r4tqz/
noindex: true
sitemap: false
---

{%- comment -%}
  Test page for the registration service in test mode (spec 2026-09-25,
  section 7.2 stage 4). The path is not secret, this repository is public;
  what makes the page harmless is the service's TEST_RECIPIENTS allow-list.
  Deleted in the go-live PR.
{%- endcomment -%}

<div class="form-outcome form-outcome--fail" markdown="1">
**Testseite.** Diese Anmeldung geht an den neuen Anmeldedienst im Testmodus. Nichts davon ist eine echte Buchung.
</div>

{% include registration-form.html lang="de" endpoint="https://arc42-registration.fly.dev/submit" %}
