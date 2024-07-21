package server

import (
	"embed"
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

	"github.com/fluxcapacitor2/easysearch/app/config"
	"github.com/fluxcapacitor2/easysearch/app/database"
)

//go:embed templates static
var content embed.FS

type httpResponse struct {
	status       int16
	Success      bool              `json:"success"`
	Error        string            `json:"error,omitempty"`
	Results      []database.Result `json:"results"`
	Pagination   paginationInfo    `json:"pagination"`
	ResponseTime float64           `json:"responseTime"`
}

type paginationInfo struct {
	Page     uint32 `json:"page"`
	PageSize uint32 `json:"pageSize"`
	Total    uint32 `json:"total"`
}

func Start(db database.Database, config *config.Config) {

	if config.ResultsPage.Enabled {

		http.Handle("/static/", http.FileServerFS(content))
		t, err := template.ParseFS(content, "templates/*.tmpl")

		if err != nil {
			panic(fmt.Sprintf("Failed to parse templates: %v\n", err))
		}

		http.HandleFunc("/{$}", func(w http.ResponseWriter, req *http.Request) {
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
				w.Header().Set("HX-Replace-URL", url.String())
			}

			renderTemplateWithResults(db, config, req, w, t, "results")
		})
	}

	http.HandleFunc("/api/search", func(w http.ResponseWriter, req *http.Request) {
		timeStart := time.Now().UnixMicro()
		var response *httpResponse

		src := req.URL.Query()["source"]
		q := req.URL.Query().Get("q")
		page, err := strconv.ParseUint(req.URL.Query().Get("page"), 10, 32)

		if q != "" && src != nil && len(src) > 0 && err == nil {
			results, total, err := db.Search(src, q, uint32(page), 10)
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

	addr := fmt.Sprintf("%v:%v", config.HTTP.Listen, config.HTTP.Port)
	fmt.Printf("Listening on http://%v\n", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

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
	database.Result
	Breadcrumbs []breadcrumb
}

type breadcrumb struct {
	URL  string
	Text string
}

type togglableSource struct {
	ID      string
	Enabled bool
}

func renderTemplateWithResults(db database.Database, config *config.Config, req *http.Request, w http.ResponseWriter, t *template.Template, templateName string) {
	src := req.URL.Query()["source"]
	q := req.URL.Query().Get("q")
	page, err := strconv.ParseUint(req.URL.Query().Get("page"),10,32)

	var results []database.Result
	var total *uint32
	var totalTime int64

	if len(src) > 0 && len(q) > 0 && err == nil {
		var err error
		start := time.Now().UnixMicro()
		results, total, err = db.Search(src, q, uint32(page), 10)
		end := time.Now().UnixMicro()

		totalTime = end - start

		if err != nil || total == nil {
			fmt.Printf("Error fetching results while serving results template: %v\n", err)
			w.WriteHeader(500)
			w.Write([]byte("Internal server error"))
			return
		}
	} else {
		results = make([]database.Result, 0)
		t := uint32(0)
		total = &t
		totalTime = 0
	}

	sources := make([]togglableSource, 0, len(config.Sources))

	for _, s := range config.Sources {
		sources = append(sources, togglableSource{
			ID:      s.ID,
			Enabled: slices.Contains(src, s.ID),
		})
	}

	pageCount := int(math.Ceil(float64(*total) / 10.0))
	if pageCount < 1 {
		pageCount = 1
	}
	pages := createPagination(req.URL, int(page), pageCount)

	mappedResults := make([]searchResult, len(results))
	for i, res := range results {
		url, err := url.Parse(res.URL)

		breadcrumbs := make([]breadcrumb, 0)
		if err == nil {
			breadcrumbs = append(breadcrumbs, breadcrumb{URL: url.Scheme + "://" + url.Host, Text: url.Host})

			for _, segment := range strings.Split(url.Path, "/") {
				if len(strings.TrimSpace(segment)) == 0 {
					continue
				}
				breadcrumbs = append(breadcrumbs, breadcrumb{URL: breadcrumbs[len(breadcrumbs)-1].URL + "/" + segment, Text: segment})
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
