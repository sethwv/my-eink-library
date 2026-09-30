---
title: Browse and download
parent: User Guide
nav_order: 1
---

# Browse, search, and download books

The Library is the home page after you sign in. It searches the books indexed from the EPUB folders configured during [Deployment](../../deployment/).

## Minimum: find and download a book

1. Enter a title, author, or series in the search field and select **Search**.
2. Open a book by selecting its cover.
3. Select **Download EPUB** for the original file, or **Download KEPUB** for a Kobo-oriented copy.

An EPUB download is the exact source file. A KEPUB download is converted when requested, so it can take longer for a large book. If your reading application accepts ordinary EPUB files, use EPUB. Use KEPUB when you specifically want Kobo's KEPUB behavior.

{% include screenshot-pair.html id="library-grid" %}

## Advanced: narrow and navigate the library

- Use **Sort by** to order results by title, author, series, date added, or release date. The adjacent ascending/descending control reverses the selected order.
- The page selector and arrows move through long result lists while preserving the search and sort choice.
- Select an author or series from a book's details to browse that group. The Authors and Series navigation entries also open their complete indexes.
- A book-detail link includes a `book` value in the address, so you can keep the page open or share the library search URL with a signed-in user. Access to the book still requires an authorized session.

## Read the book details

The details panel shows available downloads, author and series shortcuts, saved shelves, a description when the EPUB or enrichment data provides one, and the indexed file location. The location identifies which configured library folder supplied the book. It is informational only, not a file-browser link.

{% include screenshot-pair.html id="book-modal" %}

{: .note }
Metadata and cover corrections are administrator functions. See the Metadata and integrations guide when it is available rather than editing source EPUB files from this screen.
