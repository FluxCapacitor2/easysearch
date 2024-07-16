# Easysearch

A simple way to add search to your website, featuring:

- Automated crawling and indexing of your site
- Scheduled content refreshes
- Sitemap scanning
- An API for search results
- Multi-tenancy

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
