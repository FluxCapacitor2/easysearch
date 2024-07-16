# Easysearch

A simple way to add search to your website, featuring:

- Automated crawling and indexing of your site
- Scheduled content refreshes
- Sitemap scanning
- An API for search results
- Multi-tenancy

This project is built with [Go](https://go.dev/) and requires [CGo](https://go.dev/wiki/cgo) due to the [SQLite](https://www.sqlite.org/) [dependency](https://github.com/mattn/go-sqlite3).

## Why?

I wanted to add a search function to my website. When I researched my available options, I found that:

- Google [Programmable Search Engine](https://programmablesearchengine.google.com/about/) (formerly Custom Search) wasn't customizable with the prebuilt widget and costs $5 per 1000 queries via the JSON API. It also only includes pages that are indexed by Google (obviously), so my results would be incomplete.
- [Algolia](https://www.algolia.com/) is a fully-managed SaaS that you can't self-host. While its results are incredibly good, they could change their offerings or pricing model at any time. Also, crawling is an additional cost.
- A custom search solution that uses my data would return the best quality results, but it takes time to build for each site and must be updated whenever my schema changes.

This is a FOSS alternative to the aforementioned products that addresses my primary pain points. It's simple, runs anywhere, and lets you own your data.

## Alternatives

- If you have a static site, check out [Pagefind](https://pagefind.app/). It runs search on the client-side and builds an index whenever you generate your site.
- For very small, personal sites, check out [Kagi Sidekick](https://sidekick.kagi.com/) when it launches.
- If you're a nonprofit, school, or government agency, you can disable ads on your Google Programmable Search Engine. See [this article](https://support.google.com/programmable-search/answer/12423873) for more info.

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

## Configuration

Easysearch requires a config file located at `./config.yml` in the current working directory.

See the example in [config-sample.yml](https://github.com/FluxCapacitor2/easysearch/blob/main/config-sample.yml) for more information.

## Development

1. Clone the repository:

```sh
git clone https://github.com/FluxCapacitor2/easysearch
```

2. Run the app locally:

```sh
go run --tags="fts5" .
```

<small>

**Note**: When using SQLite, you have to add a build tag to enable full-text search with the [`fts5` extension](https://sqlite.org/fts5.html) (see [this section](https://github.com/mattn/go-sqlite3/tree/master?tab=readme-ov-file#feature--extension-list) of the `go-sqlite3` README for more info). That is why the `--tags="fts5"` flag is present. If you're only using Postgres, you can remove the flag for a slightly faster build.

</small>

## Building and Running an Executable

You can build a binary with this command:

```sh
go build --tags "fts5" .
```

Then, you can run it like this:

```sh
$ ./easysearch
```

If you're on Windows, the file name would be `easysearch.exe`.

## Building and Running with Docker

You can build an Easysearch Docker image with this command:

```sh
docker build . -t ghcr.io/fluxcapacitor2/easysearch:test
```

Then, to run it, use this:

```sh
docker run -p 8080:8080 -v ./config.yml:/var/run/easysearch/config.yml ghcr.io/fluxcapacitor2/easysearch:test
```

**To use the latest version from the `main` branch of this repository**, you can run:

```
docker run -p 8080:8080 -v ./config.yml:/var/run/easysearch/config.yml ghcr.io/fluxcapacitor2/easysearch:main
```

This port-forwards port `8080` and mounts `config.yml` from your current working directory into the container.

The image is built automatically with a GitHub Actions [workflow](https://github.com/FluxCapacitor2/easysearch/blob/main/.github/workflows/container.yml),
so it's always up-to-date.
