---
title: Home
nav_order: 1
description: "A self-hosted EPUB library made for e-reader browsers."
---

<div class="hero">
  <p class="eyebrow">Your books, without the cloud shelf</p>
  <h1>my-eink-library</h1>
  <p class="hero-lede">A quiet, self-hosted EPUB library for Kobo, Kindle, and the browsers they bring along.</p>
  <div class="hero-actions">
    <a class="btn btn-primary" href="{{ '/getting-started' | relative_url }}">Get started</a>
    <a class="btn btn-secondary" href="https://github.com/sethwv/my-eink-library">View source</a>
  </div>
</div>

<div class="feature-grid">
  <section class="feature-card">
    <span class="feature-mark">01</span>
    <h2>Read the files you own</h2>
    <p>Index a read-only EPUB folder. Metadata and covers come directly from each book, without a separate catalog to maintain.</p>
  </section>
  <section class="feature-card">
    <span class="feature-mark">02</span>
    <h2>Built for e-ink</h2>
    <p>Small pages, generous tap targets, and deliberately conservative browser support make it at home on Kobo's older WebKit browser.</p>
  </section>
  <section class="feature-card">
    <span class="feature-mark">03</span>
    <h2>Keep it yours</h2>
    <p>Run one container, keep your library private, and download original EPUBs or Kobo-friendly KEPUBs when you need them.</p>
  </section>
</div>

## What it does

- Serves a searchable EPUB library from one or more local directories
- Extracts titles, authors, series, covers, and other metadata from EPUB files
- Supports multiple accounts, admin controls, shelves, and password management
- Converts EPUBs to KEPUB on demand for Kobo devices
- Watches the library for changes and updates its local SQLite index

## Screenshots
[More screenshots](screenshots)

<!-- {% include screenshot-pair.html id="login" %} -->

{% include screenshot-pair.html id="library-grid" %}

{% include screenshot-pair.html id="book-modal" %}

{% include screenshot-pair.html id="library-dark" %}

<!-- {% include screenshot-pair.html id="admin-settings" %} -->

## Next step

Start with [Getting started](getting-started.md), then use [Deployment](deployment.md) when you are ready to run it beyond a local machine.
