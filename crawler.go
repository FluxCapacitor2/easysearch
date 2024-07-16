package main

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/go-shiori/go-readability"
	"github.com/gocolly/colly"
	"golang.org/x/exp/maps"
	"golang.org/x/net/html"
)

func crawl(source Source, db Database, pageUrl string) ([]string, error) {

	// Parse the URL, canonicalize it, and convert it back into a string for later use
	orig, err := url.Parse(pageUrl)

	if err != nil {
		return nil, err
	}

	parsedUrl := canonicalize(orig)
	pageUrl = parsedUrl.String()

	fmt.Printf("Crawling URL: %v\n", pageUrl)
	collector := colly.NewCollector()
	collector.IgnoreRobotsTxt = false
	collector.AllowedDomains = source.AllowedDomains

	urls := map[string]struct{}{}

	collector.OnHTML("html", func(element *colly.HTMLElement) {

		article, err := readability.FromDocument(element.DOM.Get(0), parsedUrl)
		description, _ := element.DOM.Find("meta[name=description]").Attr("content")

		if err != nil || article.TextContent == "" {
			// Readability couldn't parse the document. Instead,
			// use a simpler heuristic to find text content.

			title := element.DOM.Find("title").Text()
			content := ""
			for _, item := range element.DOM.Nodes {
				content += getText(item)
			}
			_, err = db.addDocument(source.Id, pageUrl, title, description, content)
		} else {
			_, err = db.addDocument(source.Id, pageUrl, article.Title, description, article.TextContent)
		}

		if err != nil {
			fmt.Printf("Error recording document: %v\n", err)
		}
	})

	collector.OnHTML("a[href]", func(element *colly.HTMLElement) {
		href := element.Request.AbsoluteURL(element.Attr("href"))
		parsed, err := url.Parse(href)

		if err == nil {
			url := canonicalize(parsed)
			urls[url.String()] = struct{}{}
		}
	})

	err = collector.Visit(pageUrl)

	if err != nil {
		return nil, err
	}

	collector.Wait()

	return maps.Keys(urls), nil
}

var bannedElements = []string{"head", "meta", "script", "style", "noscript", "object", "svg"}

func getText(node *html.Node) string {
	text := ""

	if node.FirstChild != nil {
		if !slices.Contains(bannedElements, node.Data) {
			text += getText(node.FirstChild)
		}
	}

	if node.Type == html.TextNode {
		fmt.Println(node.Data)
		text += node.Data + " "
	}

	if node.NextSibling != nil {
		text += getText(node.NextSibling)
	}

	return text
}

// Format URLs to keep them as consistent as possible
func canonicalize(url *url.URL) *url.URL {
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
	return url
}
