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
    <a class="btn btn-primary" href="{{ '/quick-start' | relative_url }}">Get started</a>
    <a class="btn btn-secondary" href="https://github.com/sethwv/my-eink-library/releases">Download releases</a>
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
    <h2>Run Your Way</h2>
    <p>Deploy with Docker, a Windows x64 executable, or Debian packages for x64 and 64-bit ARM Linux.</p>
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
- [Vision](project/vision.md)
- [Deployment](deployment/)
- [Roadmap](project/roadmap.md)
- [Browser Limitations](technical/browser-limitations.md)
- [Contributing](contributing.md)
