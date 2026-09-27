---
title: "Registration (test)"
layout: page
permalink: /registration-test-8r4tqz/
lang: en
translation_url: /anmeldung-test-8r4tqz/
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
**Test page.** This registration goes to the new registration service in test mode. Nothing here is a real booking.
</div>

{% include registration-form.html lang="en" endpoint=site.registration_test_endpoint %}
