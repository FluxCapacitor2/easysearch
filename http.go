package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

//go:embed frontend/default/index.html.tmpl
var searchPage string

type pageParams struct {
	CustomHTML template.HTML

	Query   string
	Sources []togglableSource

	Results []searchResult
	Time    float64

	Total uint32
	Pages []resultsPage
}

type searchResult struct {
	Result
	Breadcrumbs []breadcrumb
}

type breadcrumb struct {
	Url  string
	Text string
}

type resultsPage struct {
	Number  string
	Current bool
	Url     string
}

type togglableSource struct {
	Id      string
	Enabled bool
}

func urlWithParam(url *url.URL, key string, value string) string {
	q := url.Query()
	q.Set(key, value)
	url.RawQuery = q.Encode()
	return url.String()
}

func createPagination(req *http.Request, page int, pageCount int) []resultsPage {
	pages := make([]resultsPage, 0, int(math.Min(12, float64(pageCount+4))))

	if pageCount == 1 {
		// If there's only one page, pagination buttons should not be shown
		return pages
	}

	pages = append(pages, resultsPage{
		Number:  "«",
		Current: page == 1,
		Url:     urlWithParam(req.URL, "page", "1"),
	})

	pages = append(pages, resultsPage{
		Number:  "←",
		Current: page == 1,
		Url:     urlWithParam(req.URL, "page", strconv.Itoa(page-1)),
	})

	startIndex := page - 5

	endIndex := page + 5
	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex-startIndex < 10 {
		endIndex = startIndex + 10
	}
	if endIndex > pageCount {
		endIndex = pageCount
	}
	if endIndex-startIndex < 10 {
		startIndex = endIndex - 10
	}
	if startIndex < 0 {
		startIndex = 0
	}

	for p := range endIndex - startIndex {
		i := p + startIndex + 1

		pages = append(pages, resultsPage{
			Number:  strconv.Itoa(i),
			Current: page == i,
			Url:     urlWithParam(req.URL, "page", strconv.Itoa(i)),
		})
	}

	pages = append(pages, resultsPage{
		Number:  "→",
		Current: page == pageCount,
		Url:     urlWithParam(req.URL, "page", strconv.Itoa(page+1)),
	})

	pages = append(pages, resultsPage{
		Number:  "»",
		Current: page == pageCount,
		Url:     urlWithParam(req.URL, "page", strconv.Itoa(pageCount)),
	})

	return pages
}

func renderTemplateWithResults(db Database, config *config, req *http.Request, w http.ResponseWriter, t *template.Template, templateName string) {
	src := req.URL.Query()["source"]
	q := req.URL.Query().Get("q")
	page, err := strconv.Atoi(req.URL.Query().Get("page"))

	var results []Result
	var total *uint32
	var totalTime int64

	if len(src) > 0 && len(q) > 0 && err == nil {
		var err error
		start := time.Now().UnixMicro()
		results, total, err = db.search(src, q, uint32(page), 10)
		end := time.Now().UnixMicro()

		totalTime = end - start

		if err != nil || total == nil {
			fmt.Printf("Error fetching results while serving results template: %v\n", err)
			w.WriteHeader(500)
			w.Write([]byte("Internal server error"))
			return
		}
	} else {
		// Bad request
		w.WriteHeader(400)
		return
	}

	sources := make([]togglableSource, 0, len(config.Sources))

	for _, s := range config.Sources {
		sources = append(sources, togglableSource{
			Id:      s.Id,
			Enabled: slices.Contains(src, s.Id),
		})
	}

	pageCount := int(*total / 10)
	if pageCount < 1 {
		pageCount = 1
	}
	pages := createPagination(req, page, pageCount)

	mappedResults := make([]searchResult, len(results))
	for i, res := range results {
		url, err := url.Parse(res.Url)

		breadcrumbs := make([]breadcrumb, 0)
		if err == nil {
			breadcrumbs = append(breadcrumbs, breadcrumb{Url: url.Scheme + "://" + url.Host, Text: url.Host})

			for _, segment := range strings.Split(url.Path, "/") {
				if len(strings.TrimSpace(segment)) == 0 {
					continue
				}
				breadcrumbs = append(breadcrumbs, breadcrumb{Url: breadcrumbs[len(breadcrumbs)-1].Url + "/" + segment, Text: segment})
			}
		}

		mappedResults[i] = searchResult{
			Result:      res,
			Breadcrumbs: breadcrumbs,
		}
	}

	w.Header().Add("Content-Type", "text/html")
	err = t.ExecuteTemplate(w, templateName, &pageParams{
		Query:      q,
		Sources:    sources,
		Results:    mappedResults,
		Total:      *total,
		Time:       float64(totalTime) / 1e6,
		Pages:      pages,
		CustomHTML: template.HTML(config.ResultsPage.CustomHTML),
	})

	if err != nil {
		w.Write([]byte(fmt.Sprintf("Internal server error: %v\n", err)))
	}
}

type httpResponse struct {
	status       int16
	Success      bool           `json:"success"`
	Error        string         `json:"error,omitempty"`
	Results      []Result       `json:"results"`
	Pagination   paginationInfo `json:"pagination"`
	ResponseTime float64        `json:"responseTime"`
}

type paginationInfo struct {
	Page     uint32 `json:"page"`
	PageSize uint32 `json:"pageSize"`
	Total    uint32 `json:"total"`
}

func serve(db Database, config *config) {

	if config.ResultsPage.Enabled {
		fs := http.FileServer(http.Dir("./frontend/default/"))
		t := template.Must(template.New("index").Parse(string(searchPage)))

		http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
			renderTemplateWithResults(db, config, req, w, t, "index")
		})

		http.HandleFunc("/results", func(w http.ResponseWriter, req *http.Request) {
			// This endpoint returns results as HTML to be used on the index page (/).
			// It is called by Alpine.js to show search results without a full page reload.

			if req.Header.Get("HX-Request") != "" {
				// ^ This request was made with HTMX. Update the URL shown in the address bar to match the most recent query params.
				// https://htmx.org/docs/#response-headers
				url := req.URL
				url.Path = "/"
				w.Header().Set("HX-Replace-Url", url.String())
			}

			renderTemplateWithResults(db, config, req, w, t, "results")
		})

		http.Handle("/style.css", fs)

	}

	http.HandleFunc("/api/search", func(w http.ResponseWriter, req *http.Request) {
		timeStart := time.Now().UnixMicro()
		var response *httpResponse

		src := req.URL.Query()["source"]
		q := req.URL.Query().Get("q")
		page, err := strconv.Atoi(req.URL.Query().Get("page"))

		if q != "" && src != nil && len(src) > 0 && err == nil {
			results, total, err := db.search(src, q, uint32(page), 10)
			if err != nil {
				response = &httpResponse{
					status:  500,
					Success: false,
					Error:   "Internal server error",
				}

				fmt.Printf("Error generating search results: %v\n", err)
			} else {
				response = &httpResponse{
					status:  200,
					Success: true,
					Results: results,
					Pagination: paginationInfo{
						Page:     uint32(page),
						PageSize: 10,
						Total:    *total,
					},
				}
			}
		} else {
			response = &httpResponse{
				status:  400,
				Success: false,
				Error:   "Bad request",
			}
		}

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(int(response.status))
		response.ResponseTime = float64(time.Now().UnixMicro()-timeStart) / 1e6
		str, err := json.Marshal(response)
		if err != nil {
			w.Write([]byte(`{"success":"false","error":"Failed to marshal struct into JSON"}`))
		} else {
			w.Write([]byte(str))
		}
	})

	addr := fmt.Sprintf("%v:%v", config.Http.Listen, config.Http.Port)
	fmt.Printf("Listening on http://%v\n", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
