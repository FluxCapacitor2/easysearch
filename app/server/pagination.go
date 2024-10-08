package server

import (
	"math"
	"net/url"
	"strconv"
)

type ResultsPage struct {
	Number  string
	Current bool
	URL     string
}

func urlWithParam(url *url.URL, key string, value string) string {
	q := url.Query()
	q.Set(key, value)
	url.RawQuery = q.Encode()
	return url.String()
}

func CreatePagination(url *url.URL, page int32, pageSize int32, pageCount int32) []ResultsPage {
	pages := make([]ResultsPage, 0, int(math.Min(12, float64(pageCount+4))))

	if pageCount == 1 {
		// If there's only one page, pagination buttons should not be shown
		return pages
	}

	pages = append(pages, ResultsPage{
		Number:  "«",
		Current: page == 1,
		URL:     urlWithParam(url, "page", "1"),
	})

	pages = append(pages, ResultsPage{
		Number:  "←",
		Current: page == 1,
		URL:     urlWithParam(url, "page", strconv.FormatUint(uint64(page-1), 10)),
	})

	startIndex := page - pageSize/2
	endIndex := page + pageSize/2

	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex-startIndex < pageSize {
		endIndex = startIndex + pageSize
	}
	if endIndex > pageCount {
		endIndex = pageCount
	}
	if endIndex-startIndex < pageSize {
		startIndex = endIndex - pageSize
	}
	if startIndex < 0 {
		startIndex = 0
	}

	for p := range endIndex - startIndex {
		i := p + startIndex + 1

		pages = append(pages, ResultsPage{
			Number:  strconv.FormatUint(uint64(i), 10),
			Current: page == i,
			URL:     urlWithParam(url, "page", strconv.FormatUint(uint64(i), 10)),
		})
	}

	pages = append(pages, ResultsPage{
		Number:  "→",
		Current: page == pageCount,
		URL:     urlWithParam(url, "page", strconv.FormatUint(uint64(page+1), 10)),
	})

	pages = append(pages, ResultsPage{
		Number:  "»",
		Current: page == pageCount,
		URL:     urlWithParam(url, "page", strconv.FormatUint(uint64(pageCount), 10)),
	})

	return pages
}
