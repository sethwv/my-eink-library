---
title: Home
nav_order: 1
description: "A self-hosted EPUB library made for e-reader browsers."
---
<div class="hero">
  <!-- <p class="eyebrow"></p> -->
  <h1>my-eink-library</h1>
  <p class="hero-lede">A purpose built EPUB library for Kobo e-reader web browsers.</p>
  <div class="hero-actions">
    <a class="btn btn-primary" href="{{ '/getting-started' | relative_url }}">Get started</a>
    <a class="btn btn-secondary" href="https://github.com/sethwv/my-eink-library">View source</a>
  </div>
</div>
<div class="feature-grid">
  <section class="feature-card">
    <!-- <span class="feature-mark">01</span> -->
    <h2>Your Files</h2>
    <p>Index one or many folders of EPUB files. Metadata and covers come directly from each book, with optional enrichment from Chaptarr and/or Hardcover.</p>
  </section>
  <section class="feature-card">
    <!-- <span class="feature-mark">02</span> -->
    <h2>Built for e-ink</h2>
    <p>Scalable pages with generous tap targets, and deliberately conservative browser support keep the site usable on Kobo's older WebKit browser.</p>
  </section>
  <section class="feature-card">
    <!-- <span class="feature-mark">03</span> -->
    <h2>Written in GO</h2>
    <p>Run one container (or binary, work in progress), GO keeps things snappy and compatible however or wherever you want to run it.</p>
  </section>
</div>
- Complatible with web-browser equipped Kobo devices
- Reads book metadata and covers directly from EPUB files
- Watches one or more library directories as read-only
- Supports multiple users, admin controls, favourites
- Converts EPUBs to KEPUB on demand
- Supports Hardcover & Chaptarr Integrations
{% include screenshot-pair.html id="library-grid" %}
{% include screenshot-pair.html id="library-dark" %}
[More screenshots](screenshots)

## Explore
- [Project vision](vision.md)
- [Roadmap](roadmap.md)
- [Known limitations](limitations.md)
- [Contributing](contributing.md)
