# Easysearch

A simple way to add search to your website, featuring:

- Automated crawling and indexing of your site
- Scheduled content refreshes
- Sitemap scanning
- An API for search results
- Multi-tenancy

## Why?

I wanted to add a search function to my website. When I researched my available options, I found that:

- Google [Programmable Search Engine](https://programmablesearchengine.google.com/about/) (formerly Custom Search) wasn't customizable with the prebuilt widget and costs $5 per 1000 queries via the JSON API. It also only includes pages that are indexed by Google (obviously), so my results would be incomplete.
- [Algolia](https://www.algolia.com/) is a fully-managed SaaS that you can't self-host. While its results are incredibly good, they could change their offerings or pricing model at any time. Also, crawling is an additional cost.
- A custom search solution that uses my data would return the best quality results, but it takes time to build for each site and must be updated whenever my schema changes.

This is a FOSS alternative to the aforementioned products that addresses my primary pain points. It's simple, runs anywhere, and lets you own your data.

## Alternatives

- If you have a static site, check out [Pagefind](https://pagefind.app/). It runs search on the client-side and builds an index whenever you generate your site.
- For very small, personal sites, check out [Kagi Sidekick](https://sidekick.kagi.com/) when it launches.

## To-do list

- [ ] Canonicalization
- [ ] Build a common representation of query features (like AND, OR, exact matches, negation, fuzzy matches) and using it to build queries for the user's database driver
- [x] Implementing something like Readability (or at least removing the contents of non-text resources)
- [ ] SPA support using a headless browser
- [ ] Distributed queue (the architecture might already work with multiple instances)
- [ ] Postgres support
- [ ] MySQL support
- [ ] Prebuilt components for React, Vue, Svelte, etc.
- [ ] Exponential backoff for crawl errors

## Development

1. Clone the repository:

```
git clone https://github.com/FluxCapacitor2/easysearch
```

2. Run the app locally:

```
go run .
```

When using SQLite, you will have to add a build tag to enable full-text search with the `fts5` extension:

```
go run --tags="fts5" .
```
