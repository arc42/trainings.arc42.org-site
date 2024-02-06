---
title: "arc42 training dates"
layout: splash  
permalink: /
header:
  overlay_color: "#d7ecf8"
  overlay_filter: rgba(15, 80, 180, 0.4)
  overlay_image: /images/splash/arc42-splash-landingpage.webp
  caption: "Photo credit: [**Thomas Bayer**](https://www.predic8.de/)"
  actions: 
    - label: "Main Page"
      url: 'https://arc42.org'
    - label: "Main Page (German)"
      url: 'https://arc42.de'

excerpt: "All you ever need to construct, **communicate and document your software architecture**. Proven, **practical and pragmatic**.
Free and open source, **takes the pain out of documentation**."
---    

<script src="https://unpkg.com/htmx.org@1.9.6"
        integrity="sha384-FhXw7b6AlE/jyjlZH5iHa/tTe9EpJ1Y55RjcgPbjeWMskSxZt1v9qkxLJWNJaGni"
        crossorigin="anonymous"></script>

<div hx-get="https://arc42-subtle-ads-backend.vercel.app/api"
     hx-trigger="load delay"
     hx-swap="outerHTML"
     hx-target="#subtle-ads">
</div>

<div id="subtle-ads">
  <h4>arc42 offers architecture training.</h4>
  <p>
    The data is currently loaded from the backend and should display here shortly.
    If not, please check your internet connection. If that's not where the issue lies, 
    <a href="/contact">
      please contact us!
    </a>.
  </p>
</div>