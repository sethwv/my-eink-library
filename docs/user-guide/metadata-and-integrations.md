---
title: Metadata
parent: User Guide
nav_order: 6
---

# Metadata

The **Enrichment** admin tab can enrich indexed EPUB metadata with Hardcover or Chaptarr. It is optional. A library remains usable with the metadata and covers found in its EPUB files.

## Connect an enrichment provider

For **Hardcover**, obtain an API token from Hardcover, open **Enrichment**, select the Hardcover tab, paste the token, enable the provider, then save. For **Chaptarr**, provide its base URL and API key, enable it, then save.

After configuration, the queue reports books that are pending, completed, unmatched, or errored. Start with one provider and confirm its results before enabling additional options.

{% include screenshot-pair.html id="enrichment-hardcover" %}

## Control matching and visibility

**Hide unmatched** removes books that do not match the selected provider from the library view. Enable it only when the provider is authoritative for the collection, since an unmatched book can otherwise appear to have disappeared.

With Hardcover, **Use Hardcover covers** replaces source EPUB covers with provider covers. Leave it disabled when the original EPUB covers are preferred.

Use **Reset enrichment** to queue the collection again after changing provider configuration or when you need to retry matching. Review queue errors before repeatedly resetting it, especially if the provider endpoint or token may be unavailable.

{% include screenshot-pair.html id="enrichment-chaptarr" %}

## Edit one book

Administrators can open a book's details and select **Edit Metadata**. The page supports automatic and manual provider searches, selecting a candidate, looking up a Chaptarr path, and directly correcting title, series, index, release date, publisher, page count, ISBN, rating, genres, description, and cover behavior.

Use the manual fields to correct a single known record rather than repeatedly changing global provider settings. Confirm the result in the book details after saving. Metadata editing is visible in the current UI only to Admin users, even though Server Managers can manage the broader enrichment configuration.

{% include screenshot-pair.html id="metadata-edit" %}

{: .warning }
Provider tokens and API keys grant access to external services. Do not include them in screenshots, support requests, or shared configuration files.
