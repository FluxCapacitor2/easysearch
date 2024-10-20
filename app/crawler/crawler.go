package crawler

import (
	"bytes"
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
	"golang.org/x/exp/maps"
	"golang.org/x/net/html"
)

type CrawlResult struct {
	// The URLs discovered while visiting the page which should be added to the crawl queue.
	URLs []string
	// The canonical URL of the page, discovered by reading meta tags and following redirects.
	Canonical string
}

type pageContent struct {
	canonical   string
	status      database.QueueItemStatus
	title       string
	description string
	content     string
	errorInfo   string
}

func Crawl(source config.Source, currentDepth int32, referrer string, db database.Database, pageURL string) (*CrawlResult, error) {

	// Parse the URL, canonicalize it, and convert it back into a string for later use
	orig, err := url.Parse(pageURL)

	if err != nil {
		return nil, err
	}

	parsedURL, err := Canonicalize(source.ID, db, orig)
	if err != nil {
		return nil, err
	}

	page := pageContent{canonical: parsedURL.String(), status: database.Unindexable}

	if page.canonical != pageURL {
		fmt.Printf("Crawling URL: %v (canonicalized from %v)\n", page.canonical, pageURL)
	} else {
		fmt.Printf("Crawling URL: %v\n", page.canonical)
	}
	collector := colly.NewCollector()
	collector.IgnoreRobotsTxt = false
	collector.AllowedDomains = source.AllowedDomains

	urls := map[string]struct{}{}

	add := func(urlStr string) error {
		parsed, err := url.Parse(urlStr)
		if err != nil {
			return err
		}
		url, err := Canonicalize(source.ID, db, parsed)
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
				page.status = database.Error
				page.errorInfo = "Disallowed by <meta name=\"robots\">"
				return
			}
		}

		article, err := readability.FromDocument(element.DOM.Get(0), parsedURL)
		description, _ := element.DOM.Find("meta[name=description]").Attr("content")

		if metaCanonicalTag, exists := element.DOM.Find("link[rel=canonical]").Attr("href"); exists {
			page.canonical = metaCanonicalTag
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

		page.status = database.Finished
		page.title = strings.TrimSpace(element.DOM.Find("title").Text())
		page.description = description

		if err != nil || article.TextContent == "" {
			// Readability couldn't parse the document. Instead,
			// use a simpler heuristic to find text content.

			page.content = ""
			for _, item := range element.DOM.Nodes {
				page.content += getText(item)
			}
		} else {
			if len(page.title) == 0 {
				page.title = article.Title
			}
			page.content = article.TextContent
		}
	})

	collector.OnResponse(func(resp *colly.Response) {
		// The crawler follows redirects, so the canonical should be updated to match the final URL.
		page.canonical = resp.Request.URL.String()

		// If the crawler followed a redirect from an unindexed document to an indexed document,
		// parsing and adding it to the DB is unnecessary. We can just record the redirect as a canonical.
		if origExists, _ := db.HasDocument(source.ID, pageURL); origExists != nil && !*origExists {
			if exists, _ := db.HasDocument(source.ID, page.canonical); exists != nil && *exists {
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
				return add(entry.GetLocation())
			})
			reader.Reset(resp.Body)
			sitemap.ParseIndex(reader, func(entry sitemap.IndexEntry) error {
				return add(entry.GetLocation())
			})
		} else if strings.HasPrefix(ct, "application/rss+xml") || strings.HasPrefix(ct, "application/feed+json") || strings.HasPrefix(ct, "application/atom+xml") {
			// Parse RSS, Atom, and JSON feeds using `gofeed`
			parser := gofeed.NewParser()
			res, err := parser.ParseString(string(resp.Body))
			if err != nil {
				page.status = database.Error
				page.errorInfo = "Invalid feed content"
			}
			for _, item := range res.Items {
				for _, link := range item.Links {
					add(link)
				}
			}
		}
	})

	collector.OnHTML("a[href]", func(element *colly.HTMLElement) {
		href := element.Request.AbsoluteURL(element.Attr("href"))
		add(href)
	})

	err = collector.Visit(page.canonical)

	if err != nil {
		page.errorInfo = err.Error()
	}

	collector.Wait()

	result := &CrawlResult{
		URLs:      maps.Keys(urls),
		Canonical: page.canonical,
	}

	if page.canonical != pageURL {
		err := db.SetCanonical(source.ID, pageURL, page.canonical)
		if err != nil {
			fmt.Printf("Failed to set canonical URL of page %v to %v: %v\n", pageURL, page.canonical, err)
		}
	}

	if !cancelled {
		addDocErr := db.AddDocument(source.ID, currentDepth, referrer, page.canonical, page.status, page.title, page.description, page.content, page.errorInfo)
		if addDocErr != nil {
			err = addDocErr
		}
	}

	return result, err
}

var nonTextElements = []string{"head", "meta", "script", "style", "noscript", "object", "svg"}

func getText(node *html.Node) string {
	text := ""

	if node.FirstChild != nil {
		if !slices.Contains(nonTextElements, node.Data) {
			text += getText(node.FirstChild) + " "
		}
	}

	if node.Type == html.TextNode {
		text += node.Data + " "
	}

	if node.NextSibling != nil {
		text += getText(node.NextSibling) + " "
	}

	return strings.TrimSpace(text)
}

// Format URLs to keep them as consistent as possible
func Canonicalize(src string, db database.Database, url *url.URL) (*url.URL, error) {

	// Check if we already have a canonical URL recorded
	canonical, err := db.GetCanonical(src, url.String())

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
