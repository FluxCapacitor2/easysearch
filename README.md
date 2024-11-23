# Easysearch

A simple way to add search to your website, featuring:

- Automated crawling and indexing of your site
- Scheduled content refreshes
- Sitemap scanning
- An API for search results
- Multi-tenancy
- Vector similarity search, allowing search by semantic meaning instead of exact matches ([what is this?](https://www.ibm.com/topics/vector-search))
- A prebuilt search page that works without JavaScript (with [progressive enhancement](https://developer.mozilla.org/en-US/docs/Glossary/Progressive_Enhancement) for users with JS enabled)

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

- [x] Basic canonicalization
- [ ] Build a common representation of query features (like AND, OR, exact matches, negation, fuzzy matches) and using it to build queries for the user's database driver
- [x] Implementing something like Readability (or at least removing the contents of non-text resources)
- [ ] SPA support using a headless browser
- [x] Guarantee that pages in the queue are only crawled once, even in distributed scenarios
- [ ] Prebuilt components for React, Vue, Svelte, etc.
- [ ] Exponential backoff for crawl errors
- [x] Vector search
- [ ] Generating and indexing transcripts of video and audio recordings?
- [ ] Image search?

## Configuration

Easysearch requires a config file located at `./config.yml` in the current working directory.

See the example in [config-sample.yml](https://github.com/FluxCapacitor2/easysearch/blob/main/config-sample.yml) for more information.

**Note**: If you are using an API for vector embeddings that does not require an API key, you still need to add the `OPENAI_API_KEY` environment variable. It cannot be empty, but it can be any random string.

## Development

1. Clone the repository:

```sh
git clone https://github.com/FluxCapacitor2/easysearch
```

2. Run the app locally:

```sh
go run --tags="fts5" ./app
```

_If you are using VS Code, you can press F5 to run the project automatically._

<small>

**Note**: You have to add the `fts5` Go build tag to enable full-text search with the [`fts5` extension](https://sqlite.org/fts5.html), which Easysearch requires. See [this section](https://github.com/mattn/go-sqlite3/tree/master?tab=readme-ov-file#feature--extension-list) of the `go-sqlite3` README for more info.

</small>

3. Make sure you have a `sqlite3` development package installed on your system that provides `sqlite3.h`. For example:

   - Fedora/RHEL: `dnf install libsqlite3x-devel`
   - Ubuntu/Debian: `apt install libsqlite3-dev`

   If your system does not provide such a package, you can run Go builds in a Docker container and use one of the commands above to install the required package.

For automatic code formatting, make sure you have [Node.js](https://nodejs.org/) installed. Then, install [Prettier](https://prettier.io/) via NPM:

```
npm install
```

You can now format the HTML template using `prettier -w .` or enable the recommended VS Code extension to format whenever you save. This will also install a Git hook that formats Go and Go template files before committing.

For Go source files, instead of Prettier, use `go fmt`. You can format the whole source tree with `go fmt ./app/...`.

## Building and Running an Executable

You can build a binary with this command:

```sh
go build --tags "fts5" -o easysearch ./app
```

Then, you can run it like this:

```sh
$ ./easysearch
```

If you're on Windows, the file name would be `easysearch.exe` instead of `easysearch`.

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

## Search Results API

When you start up Easysearch, the API server address is printed to the process's standard output.

To get search results, make a GET request to the `/search` endpoint with the following URL parameters:

- **`source`**: The ID of your search source. This must match the value of one of the `id` properties in your `config.yml` file.
- **`q`**: Your search query.
- **`page`**: The page number, starting at 1. Each page will contain 10 results.

For example:

```
GET http://localhost:8080/search?source=brendan&q=typescript
```

The response is returned as a JSON object.

```json
{
  "success": true,
  "results": [
    {
      "url": "https://www.bswanson.dev/blog/nextauth-oauth-passing-errors-to-the-client/",
      "title": [
        {
          "highlighted": false,
          "content": "Passing user-friendly NextAuth v5 error messages to the client"
        }
      ],
      "description": [
        { "highlighted": false, "content": "In Auth.js v5, you can only pass…" }
      ],
      "content": [
        { "highlighted": false, "content": "…First, if you’re using " },
        { "highlighted": true, "content": "TypeScript" },
        {
          "highlighted": false,
          "content": ", augment the JWT and Session interfaces:\nsrc/auth.ts// This can be anything, just make sure the same…"
        }
      ],
      "rank": -3.657958588047788
    }
  ],
  "pagination": { "page": 1, "pageSize": 10, "total": 1 },
  "responseTime": 0.000778
}
```

- `results`: The list of search results
  - `url`: The canonical URL of the matching page
  - `title`: A snippet of the page title, taken from the `<title>` HTML tag
  - `description`: A snippet of the page's meta description, taken from the `<meta name="description">` HTML tag
  - `content`: A snippet of the page's text content. Text is parsed using [go-readability](https://github.com/go-shiori/go-readability) by default. If Readability doesn't find an article, text is taken from all elements except those on [this list](https://github.com/FluxCapacitor2/easysearch/blob/97ac9963390ab7bce2f886a60033e2e4dfda08cd/crawler.go#L168).
  - `rank`: The relative ranking of the item. **Lower numbers indicate greater relevance** to the search query.
- `pagination`:
  - `page`: The page specified in the request.
  - `pageSize`: The maximum amount of items returned. Currently, this value is always 10.
  - `total`: The total amount of results that match the query. The amount of pages can be computed by dividing the `total` by the `pageSize`.
- `responseTime`: The amount of time, in seconds, that it took to process the request.

`title`, `description`, and `content` are arrays. If an item is `highlighted`, then it directly matches the query. This allows you to bold relevant keywords in search results when building a user interface.

If there was an error processing the request, the response will look like this:

```json
{ "success": false, "error": "Internal server error" }
```

Error messages are intentionally vague to obscure details about your environment or database schema.
However, full errors are printed to the process's standard output.
