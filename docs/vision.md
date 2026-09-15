---
title: Project vision
nav_order: 5
---

# Project vision

my-eink-library is a self-hosted EPUB library for people who want to read the files they own on the e-readers they already use.

## What we are

- A small, dependable library server that indexes read-only EPUB folders.
- A browser interface designed first for Kobo-class e-reader browsers, then for modern desktop and mobile browsers.
- A local-first tool: your EPUB files, index, users, covers, and server settings remain under your control.

## How we work

- Prefer simple deployment: one container or one Go binary, with no required cloud service.
- Read metadata and covers from EPUBs first. Optional integrations can enrich a library but are never required to browse it.
- Favor conservative HTML, CSS, and JavaScript where older e-reader engines need it. Modern enhancements must leave a usable baseline behind.
- Keep library source folders read-only. Application state lives separately and can be recreated or backed up independently.
- Make browsing practical on e-ink: restrained pages, large touch areas, simple controls, pagination, and direct EPUB or KEPUB downloads.
- Design for monochrome e-readers. Color can support the interface, but meaning and state must not depend on color alone.

## What we are not

my-eink-library is not a cloud bookshelf, a DRM service, or a replacement for an e-reader's native library. It is not a downloader and does not discover, acquire, source, or manage book files. It also does not replace applications that interface with indexers or other acquisition services. It is a private, browser-accessible home for an EPUB collection you already have.

See [Known limitations](limitations.md) for the browser trade-offs that shape these choices and [Roadmap](roadmap.md) for current priorities.
