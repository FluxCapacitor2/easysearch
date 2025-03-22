package crawler

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/fluxcapacitor2/easysearch/app/config"
	"github.com/fluxcapacitor2/easysearch/app/database"
	"github.com/go-shiori/go-readability"
	"github.com/gocolly/colly"
	"github.com/mmcdole/gofeed"
	sitemap "github.com/oxffaa/gopher-parse-sitemap"
	slogctx "github.com/veqryn/slog-context"
	"golang.org/x/exp/maps"
	"golang.org/x/net/html"
)

type CrawlResult struct {
	// The URLs discovered while visiting the page which should be added to the crawl queue.
	URLs []string
	// The canonical URL of the page, discovered by reading meta tags and following redirects.
	Canonical string
	// The content that was extracted from the page
	Content ExtractedPageContent
	// The ID of the page that was created or updated in the database
	PageID int64
}

type ExtractedPageContent struct {
	Canonical   string
	Status      database.QueueItemStatus
	Title       string
	Description string
	Content     string
	ErrorInfo   string
}

func Crawl(ctx context.Context, source config.Source, currentDepth int32, referrers []int64, db database.Database, pageURL string) (*CrawlResult, error) {

	// Parse the URL, canonicalize it, and convert it back into a string for later use
	orig, err := url.Parse(pageURL)

	if err != nil {
		return nil, err
	}

	parsedURL, err := Canonicalize(ctx, source.ID, db, orig)
	if err != nil {
		return nil, err
	}

	page := ExtractedPageContent{Canonical: parsedURL.String(), Status: database.Unindexable}

	slogctx.Info(ctx, "Crawling URL", "canonical", page.Canonical, "original", pageURL)

	collector := colly.NewCollector()
	collector.UserAgent = "Easysearch (+https://github.com/FluxCapacitor2/easysearch)"
	collector.IgnoreRobotsTxt = false
	collector.AllowedDomains = source.AllowedDomains

	urls := map[string]struct{}{}

	add := func(urlStr string) error {
		parsed, err := url.Parse(urlStr)
		if err != nil {
			return err
		}
		url, err := Canonicalize(ctx, source.ID, db, parsed)
		if err == nil {
			urls[url.String()] = struct{}{}
		}
		return nil
	}

	cancelled := false

	collector.OnHTML("html", func(element *colly.HTMLElement) {

		if cancelled {
			return
		}

		// Make sure the page doesn't disallow indexing
		if robotsTag, exists := element.DOM.Find("meta[name=robots]").Attr("content"); exists {
			if strings.Contains(robotsTag, "noindex") || strings.Contains(robotsTag, "none") {
				page.Status = database.Error
				page.ErrorInfo = "Disallowed by <meta name=\"robots\">"
				return
			}
		}

		article, err := readability.FromDocument(element.DOM.Get(0), parsedURL)
		description, _ := element.DOM.Find("meta[name=description]").Attr("content")

		if metaCanonicalTag, exists := element.DOM.Find("link[rel=canonical]").Attr("href"); exists {
			page.Canonical = element.Request.AbsoluteURL(metaCanonicalTag)
		}

		// Find alternate links for RSS feeds, other languages, etc.
		linkTags := element.DOM.Find("link[rel=alternate]")
		linkTags.Each(func(i int, link *goquery.Selection) {

			linkType, exists := link.Attr("type")

			if exists && (linkType == "application/atom+xml" || linkType == "application/rss+xml" || linkType == "text/html") {
				href, exists := link.Attr("href")
				if exists {
					add(element.Request.AbsoluteURL(href))
				}
			}
		})

		// If we can parse the Readability output as HTML, get the text content using our method.
		// This will add spaces between HTML elements.
		if node, err := html.Parse(strings.NewReader(article.Content)); err == nil {
			article.TextContent = getText(node)
		}

		page.Status = database.Finished
		page.Title = strings.TrimSpace(element.DOM.Find("title").Text())
		page.Description = description

		if err != nil || article.TextContent == "" {
			// Readability couldn't parse the document. Instead,
			// use a simpler heuristic to find text content.

			page.Content = ""
			for _, item := range element.DOM.Nodes {
				page.Content += getText(item)
			}
		} else {
			if len(page.Title) == 0 {
				page.Title = article.Title
			}
			page.Content = article.TextContent
		}
	})

	collector.OnResponse(func(resp *colly.Response) {
		// The crawler follows redirects, so the canonical should be updated to match the final URL.
		page.Canonical = resp.Request.URL.String()

		// If the crawler followed a redirect from an unindexed document to an indexed document,
		// parsing and adding it to the DB is unnecessary. We can just record the redirect as a canonical.
		if origExists, _ := db.HasDocument(ctx, source.ID, pageURL); origExists != nil && !*origExists {
			if exists, _ := db.HasDocument(ctx, source.ID, page.Canonical); exists != nil && *exists {
				cancelled = true
				return
			}
		}

		ct := resp.Headers.Get("Content-Type")
		// XML files could be sitemaps
		if strings.HasPrefix(ct, "application/xml") || strings.HasPrefix(ct, "text/xml") {
			// Attempt to parse this response as a sitemap or sitemap index
			reader := bytes.NewReader(resp.Body)
			sitemap.Parse(reader, func(entry sitemap.Entry) error {
				return add(resp.Request.AbsoluteURL(entry.GetLocation()))
			})
			reader.Reset(resp.Body)
			sitemap.ParseIndex(reader, func(entry sitemap.IndexEntry) error {
				return add(resp.Request.AbsoluteURL(entry.GetLocation()))
			})
		} else if strings.HasPrefix(ct, "application/rss+xml") || strings.HasPrefix(ct, "application/feed+json") || strings.HasPrefix(ct, "application/atom+xml") {
			// Parse RSS, Atom, and JSON feeds using `gofeed`
			parser := gofeed.NewParser()
			res, err := parser.ParseString(string(resp.Body))
			if err != nil {
				page.Status = database.Error
				page.ErrorInfo = "Invalid feed content"
			}
			for _, item := range res.Items {
				for _, link := range item.Links {
					add(resp.Request.AbsoluteURL(link))
				}
			}
		}
	})

	collector.OnHTML("a[href]", func(element *colly.HTMLElement) {
		rel := element.Attr("rel")
		if strings.Contains(rel, "nofollow") {
			return
		}
		href := element.Request.AbsoluteURL(element.Attr("href"))
		add(href)
	})

	err = collector.Visit(page.Canonical)

	if err != nil {
		page.Status = database.Error
		page.ErrorInfo = err.Error()
	}

	collector.Wait() // This waits at most 10 seconds, which is the default request timeout

	result := &CrawlResult{
		URLs:      maps.Keys(urls),
		Canonical: page.Canonical,
		Content:   page,
	}

	if page.Canonical != pageURL {
		err := db.SetCanonical(ctx, source.ID, pageURL, page.Canonical)
		if err != nil {
			return result, fmt.Errorf("failed to set canonical URL of page %v to %v: %v", pageURL, page.Canonical, err)
		}
	}

	if !cancelled {
		text := Truncate(source.SizeLimit, page.Title, page.Description, page.Content)
		id, addDocErr := db.AddDocument(ctx, source.ID, currentDepth, referrers, page.Canonical, page.Status, text[0], text[1], text[2], page.ErrorInfo)
		result.PageID = id
		if addDocErr != nil {
			err = addDocErr
		}
	}

	return result, err
}

func Truncate(max int, items ...string) []string {
	ret := make([]string, len(items))
	remaining := max

	for i, item := range items {
		if len(item) <= remaining {
			ret[i] = item
			remaining -= len(item)
		} else if remaining > 0 {
			added := item[:remaining]
			ret[i] = added
			remaining -= len(added)
		} else {
			ret[i] = ""
		}
	}

	return ret
}

// A list of elements that will never contain useful text and should always be filtered out when collecting text content.
var nonTextElements = []string{"head", "meta", "script", "style", "noscript", "object", "svg"}

// A list of all elements in the Chromium user-agent stylesheet with the `display: block` rule.
var blockLevelElements = []string{
	// Source: https://github.com/chromium/chromium/blob/main/third_party/blink/renderer/core/html/resources/html.css
	"html", "body", "p", "address", "article", "aside", "div", "footer", "header", "hgroup", "main", "nav", "section", "blockquote", "figcaption", "figure", "center", "hr", "h1", "h2", "h3", "h4", "h5", "h6", "tr", "ul", "ol", "dd", "dl", "dt", "menu", "dir", "form", "legend", "fieldset", "optgroup", "option", "pre", "xmp", "plaintext", "listing", "dialog",
	// Technically, list items aren't block-level elements, but they do create a new line.
	"li",
}

func getText(node *html.Node) string {
	text := ""

	if node.FirstChild != nil {
		if !slices.Contains(nonTextElements, node.Data) {
			isBlock := node.Type == html.ElementNode && slices.Contains(blockLevelElements, node.Data)
			if isBlock {
				text += "\n"
			}
			text += getText(node.FirstChild) + " "
			if isBlock {
				text += "\n"
			}
		}
	}

	if node.Type == html.TextNode {
		text += node.Data + " "
	}

	for _, attr := range node.Attr {
		if attr.Key == "title" && strings.TrimSpace(attr.Val) != "" {
			text += attr.Val + " "
		}
	}

	if node.Type == html.ElementNode && node.Data == "img" {
		for _, attr := range node.Attr {
			if attr.Key == "alt" && strings.TrimSpace(attr.Val) != "" {
				text += attr.Val + "\n"
				break
			}
		}
	}

	if node.NextSibling != nil {
		text += getText(node.NextSibling) + " "
	}

	return strings.TrimSpace(text)
}

// Format URLs to keep them as consistent as possible
func Canonicalize(ctx context.Context, src string, db database.Database, url *url.URL) (*url.URL, error) {

	// Check if we already have a canonical URL recorded
	canonical, err := db.GetCanonical(ctx, src, url.String())

	if err != nil {
		return nil, err
	}

	if canonical != nil {
		return url.Parse(canonical.Canonical)
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
