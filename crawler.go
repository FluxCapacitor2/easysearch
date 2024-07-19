package main

import (
	"bytes"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-shiori/go-readability"
	"github.com/gocolly/colly"
	"github.com/mmcdole/gofeed"
	sitemap "github.com/oxffaa/gopher-parse-sitemap"
	"golang.org/x/exp/maps"
	"golang.org/x/net/html"
)

type CrawlResult struct {
	// The URLs discovered while visiting the page which should be added to the crawl queue.
	urls []string
	// The canonical URL of the page, discovered by reading meta tags and following redirects.
	canonical string
}

func crawl(source Source, currentDepth int32, db Database, pageUrl string) (*CrawlResult, error) {

	// Parse the URL, canonicalize it, and convert it back into a string for later use
	orig, err := url.Parse(pageUrl)

	if err != nil {
		return nil, err
	}

	parsedUrl, err := canonicalize(source.Id, db, orig)
	if err != nil {
		return nil, err
	}
	pageUrl = parsedUrl.String()

	fmt.Printf("Crawling URL: %v\n", pageUrl)
	collector := colly.NewCollector()
	collector.IgnoreRobotsTxt = false
	collector.AllowedDomains = source.AllowedDomains

	urls := map[string]struct{}{}

	canonical := pageUrl

	add := func(urlStr string) error {
		parsed, err := url.Parse(urlStr)
		if err != nil {
			return err
		}
		url, err := canonicalize(source.Id, db, parsed)
		if err == nil {
			urls[url.String()] = struct{}{}
		}
		return nil
	}

	collector.OnHTML("html", func(element *colly.HTMLElement) {

		article, err := readability.FromDocument(element.DOM.Get(0), parsedUrl)
		description, _ := element.DOM.Find("meta[name=description]").Attr("content")

		{
			metaCanonicalTag, exists := element.DOM.Find("link[rel=canonical]").Attr("href")

			if exists {
				canonical = metaCanonicalTag
			}
		}

		{
			// Find alternate links for RSS feeds, other languages, etc.
			links := element.DOM.Find("link[rel=alternate]")
			links.Each(func(i int, link *goquery.Selection) {

				linkType, exists := link.Attr("type")

				if exists && (linkType == "application/atom+xml" || linkType == "application/rss+xml" || linkType == "text/html") {
					href, exists := link.Attr("href")
					if exists {
						add(element.Request.AbsoluteURL(href))
					}
				}
			})
		}

		title := strings.TrimSpace(element.DOM.Find("title").Text())

		if err != nil || article.TextContent == "" {
			// Readability couldn't parse the document. Instead,
			// use a simpler heuristic to find text content.

			content := ""
			for _, item := range element.DOM.Nodes {
				content += getText(item)
			}
			_, err = db.addDocument(source.Id, currentDepth, canonical, Finished, title, description, content)
		} else {

			if len(title) == 0 {
				title = article.Title
			}

			_, err = db.addDocument(source.Id, currentDepth, canonical, Finished, title, description, article.TextContent)
		}

		if err != nil {
			fmt.Printf("Error recording document: %v\n", err)
		}
	})

	collector.OnResponse(func(resp *colly.Response) {
		// The crawler follows redirects, so the canonical should be updated to match the final URL.
		canonical = resp.Request.URL.String()

		ct := resp.Headers.Get("Content-Type")
		// XML files could be sitemaps
		if strings.HasPrefix(ct, "application/xml") || strings.HasPrefix(ct, "text/xml") {
			// Attempt to parse this response as a sitemap or sitemap index
			reader := bytes.NewReader(resp.Body)
			sitemap.Parse(reader, func(entry sitemap.Entry) error {
				return add(entry.GetLocation())
			})
			reader.Reset(resp.Body)
			sitemap.ParseIndex(reader, func(entry sitemap.IndexEntry) error {
				return add(entry.GetLocation())
			})
		} else if strings.HasPrefix(ct, "application/rss+xml") || strings.HasPrefix(ct, "application/feed+json") || strings.HasPrefix(ct, "application/atom+xml") {
			// Parse RSS, Atom, and JSON feeds using `gofeed`
			parser := gofeed.NewParser()
			res, _ := parser.ParseString(string(resp.Body))
			for _, item := range res.Items {
				for _, link := range item.Links {
					add(link)
				}
			}
		} else {
			return
		}

		if len(urls) > 0 { // <- This will be true if URLs were found *before* the HTML document was indexed, which only happens for sitemaps/feeds.
			// This page is a sitemap. Insert an "unindexable" document, which records that this document has been crawled, but has no text content of its own.
			db.addDocument(source.Id, currentDepth, canonical, Unindexable, "", "", "")
		}
	})

	collector.OnHTML("a[href]", func(element *colly.HTMLElement) {
		href := element.Request.AbsoluteURL(element.Attr("href"))
		add(href)
	})

	err = collector.Visit(pageUrl)

	collector.Wait()

	result := &CrawlResult{}
	result.urls = maps.Keys(urls)
	result.canonical = canonical

	if canonical != pageUrl {
		err := db.setCanonical(source.Id, pageUrl, canonical)
		if err != nil {
			fmt.Printf("Failed to set canonical URL of page %v to %v: %v\n", pageUrl, canonical, err)
		}
	}

	return result, err
}

var nonTextElements = []string{"head", "meta", "script", "style", "noscript", "object", "svg"}

func getText(node *html.Node) string {
	text := ""

	if node.FirstChild != nil {
		if !slices.Contains(nonTextElements, node.Data) {
			text += getText(node.FirstChild)
		}
	}

	if node.Type == html.TextNode {
		fmt.Println(node.Data)
		text += node.Data + " "
	}

	if node.NextSibling != nil {
		text += getText(node.NextSibling) + " "
	}

	return strings.TrimSpace(text)
}

// Format URLs to keep them as consistent as possible
func canonicalize(src string, db Database, url *url.URL) (*url.URL, error) {

	// Check if we already have a canonical URL recorded
	canonical, _ := db.getCanonical(src, url.String())

	if canonical != nil {
		return url.Parse(canonical.canonical)
	}

	// If not, try to make an educated guess:

	// Strip trailing slashes
	url.Path = strings.TrimSuffix(url.Path, "/")

	if url.Fragment != "" {
		// Remove URL fragments unless they contains slashes.
		// (If they contain a slash, they might be necessary for client-side routing, so we leave them alone)
		if !strings.Contains(url.Fragment, "/") {
			url.Fragment = ""
		}
	}

	// When the URL is converted back into a string, its query parameters will automatically be sorted by key
	return url, nil
}
