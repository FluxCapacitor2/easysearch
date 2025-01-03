package server

import (
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fluxcapacitor2/easysearch/app/config"
	"github.com/fluxcapacitor2/easysearch/app/database"
	"github.com/fluxcapacitor2/easysearch/app/embedding"
	slogctx "github.com/veqryn/slog-context"
)

//go:embed templates static
var content embed.FS

type paginationInfo struct {
	Page     uint32 `json:"page"`
	PageSize uint32 `json:"pageSize"`
	Total    uint32 `json:"total"`
}

func Start(db database.Database, cfg *config.Config) {

	if cfg.ResultsPage.Enabled {

		http.Handle("/static/", http.FileServerFS(content))
		t, err := template.ParseFS(content, "templates/*.tmpl")

		if err != nil {
			panic(fmt.Sprintf("Failed to parse templates: %v\n", err))
		}

		http.HandleFunc("/{$}", func(w http.ResponseWriter, req *http.Request) {
			renderTemplateWithResults(db, cfg, req, w, t, "index")
		})

		http.HandleFunc("/results", func(w http.ResponseWriter, req *http.Request) {
			// This endpoint returns results as HTML to be used on the index page (/).
			// It is called by HTMX to show search results without a full page reload.

			if req.Header.Get("HX-Request") != "" {
				// ^ This request was made with HTMX. Update the URL shown in the address bar to match the most recent query params.
				// https://htmx.org/docs/#response-headers
				url := req.URL
				url.Path = "/"
				w.Header().Set("HX-Replace-URL", url.String())
			}

			renderTemplateWithResults(db, cfg, req, w, t, "results")
		})
	}

	http.HandleFunc("/api/search", func(w http.ResponseWriter, req *http.Request) {
		type httpResponse struct {
			status       int16
			Success      bool                 `json:"success"`
			Error        string               `json:"error,omitempty"`
			Results      []database.FTSResult `json:"results"`
			Pagination   paginationInfo       `json:"pagination"`
			ResponseTime float64              `json:"responseTime"`
		}

		timeStart := time.Now().UnixMicro()

		respond := func(response httpResponse) {
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(int(response.status))
			response.ResponseTime = float64(time.Now().UnixMicro()-timeStart) / 1e6
			str, err := json.Marshal(response)
			if err != nil {
				w.Write([]byte(`{"success":"false","error":"Failed to marshal struct into JSON"}`))
			} else {
				w.Write([]byte(str))
			}
		}

		src := req.URL.Query()["source"]
		q := req.URL.Query().Get("q")
		page, err := strconv.ParseUint(req.URL.Query().Get("page"), 10, 32)

		if q == "" || src == nil || len(src) == 0 || err != nil {
			respond(httpResponse{
				status:  400,
				Success: false,
				Error:   "Bad request",
			})
			return
		}

		spellchecked, err := db.Spellfix(req.Context(), q)
		if err != nil {
			slogctx.Error(req.Context(), "Failed to spellcheck query", "error", err)
			spellchecked = q
		}

		results, total, err := db.Search(req.Context(), src, spellchecked, uint32(page), 10)
		if err != nil {
			respond(httpResponse{
				status:  500,
				Success: false,
				Error:   "Internal server error",
			})

			slogctx.Error(req.Context(), "Failed to generate search results", "error", err)
			return
		} else {
			respond(httpResponse{
				status:  200,
				Success: true,
				Results: results,
				Pagination: paginationInfo{
					Page:     uint32(page),
					PageSize: 10,
					Total:    *total,
				},
			})
			return
		}
	})

	http.HandleFunc("/api/similarity-search", func(w http.ResponseWriter, req *http.Request) {
		type httpResponse struct {
			status       int16
			Success      bool                        `json:"success"`
			Error        string                      `json:"error,omitempty"`
			Results      []database.SimilarityResult `json:"results"`
			ResponseTime float64                     `json:"responseTime"`
		}

		timeStart := time.Now().UnixMicro()

		respond := func(response httpResponse) {
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(int(response.status))
			response.ResponseTime = float64(time.Now().UnixMicro()-timeStart) / 1e6
			str, err := json.Marshal(response)
			if err != nil {
				w.Write([]byte(`{"success":"false","error":"Failed to marshal struct into JSON"}`))
			} else {
				w.Write([]byte(str))
			}
		}

		src := req.URL.Query()["source"]
		q := req.URL.Query().Get("q")

		if q == "" || src == nil || len(src) == 0 {
			respond(httpResponse{
				status:  400,
				Success: false,
				Error:   "Bad request",
			})
			return
		}

		foundSources := make([]config.Source, 0, len(src))

		for _, sourceID := range src {
			for _, s := range cfg.Sources {
				if s.ID == sourceID {
					foundSources = append(foundSources, s)
					break
				}
			}
		}

		queryEmbeds := make(map[string][]float32)

		for _, s := range foundSources {
			if s.Embeddings.Enabled && queryEmbeds[s.Embeddings.Model] == nil {
				vector, err := embedding.GetEmbeddings(req.Context(), s.Embeddings.OpenAIBaseURL, s.Embeddings.Model, s.Embeddings.APIKey, []string{q})
				if err != nil {
					slogctx.Error(req.Context(), "Failed to generate embeddings for query", "error", err)

					respond(httpResponse{
						status:  500,
						Success: false,
						Error:   "Internal server error",
					})
					return
				}
				queryEmbeds[s.Embeddings.Model] = vector[0]
			}
		}

		allResults := make([]database.SimilarityResult, 0)

		for _, s := range foundSources {
			results, err := db.SimilaritySearch(req.Context(), s.ID, queryEmbeds[s.Embeddings.Model], 10)
			if err != nil {
				slogctx.Error(req.Context(), "Failed to generate search results", "error", err)

				respond(httpResponse{
					status:  500,
					Success: false,
					Error:   "Internal server error",
				})
				return
			}
			allResults = append(allResults, results...)
		}

		slices.SortFunc(allResults, func(a database.SimilarityResult, b database.SimilarityResult) int {
			return cmp.Compare(a.Similarity, b.Similarity)
		})

		respond(httpResponse{
			status:  200,
			Success: true,
			Results: allResults,
		})
	})

	http.HandleFunc("/api/hybrid-search", func(w http.ResponseWriter, req *http.Request) {
		type httpResponse struct {
			status       int16
			Success      bool                    `json:"success"`
			Error        string                  `json:"error,omitempty"`
			Results      []database.HybridResult `json:"results"`
			ResponseTime float64                 `json:"responseTime"`
		}

		timeStart := time.Now().UnixMicro()

		respond := func(response httpResponse) {
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(int(response.status))
			response.ResponseTime = float64(time.Now().UnixMicro()-timeStart) / 1e6
			str, err := json.Marshal(response)
			if err != nil {
				w.Write([]byte(`{"success":"false","error":"Failed to marshal struct into JSON"}`))
			} else {
				w.Write([]byte(str))
			}
		}

		src := req.URL.Query()["source"]
		q := req.URL.Query().Get("q")

		if q == "" || src == nil || len(src) == 0 {
			respond(httpResponse{
				status:  400,
				Success: false,
				Error:   "Bad request",
			})
			return
		}

		foundSources := make([]config.Source, 0, len(src))

		for _, sourceID := range src {
			for _, s := range cfg.Sources {
				if s.ID == sourceID {
					foundSources = append(foundSources, s)
					break
				}
			}
		}

		queryEmbeds := make(map[string][]float32)

		for _, s := range foundSources {
			if s.Embeddings.Enabled && queryEmbeds[s.Embeddings.Model] == nil {
				vector, err := embedding.GetEmbeddings(req.Context(), s.Embeddings.OpenAIBaseURL, s.Embeddings.Model, s.Embeddings.APIKey, []string{q})
				if err != nil {
					slogctx.Error(req.Context(), "Failed to generate embeddings for search query", "error", err)

					respond(httpResponse{
						status:  500,
						Success: false,
						Error:   "Internal server error",
					})
					return
				}
				queryEmbeds[s.Embeddings.Model] = vector[0]
			}
		}

		embeddedQueries := make(map[string][]float32)

		for _, s := range foundSources {
			if s.Embeddings.Enabled {
				embeddedQueries[s.ID] = queryEmbeds[s.Embeddings.Model]
			}
		}

		sourceList := make([]string, 0)
		for _, s := range foundSources {
			sourceList = append(sourceList, s.ID)
		}

		if len(sourceList) == 0 {
			respond(httpResponse{
				status:  400,
				Success: false,
				Error:   "No valid sources found",
			})
			return
		}

		spellchecked, err := db.Spellfix(req.Context(), q)
		if err != nil {
			slogctx.Error(req.Context(), "Failed to spellcheck query", "error", err)
			spellchecked = q
		}

		results, err := db.HybridSearch(req.Context(), sourceList, spellchecked, embeddedQueries, 10)
		if err != nil {
			slogctx.Error(req.Context(), "Failed to generate search results", "error", err)

			respond(httpResponse{
				status:  500,
				Success: false,
				Error:   "Internal server error",
			})
			return
		}

		respond(httpResponse{
			status:  200,
			Success: true,
			Results: results,
		})
	})

	addr := fmt.Sprintf("%v:%v", cfg.HTTP.Listen, cfg.HTTP.Port)
	slog.Info("HTTP server is listening", "address", "http://"+addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

type pageParams struct {
	CustomHTML template.HTML

	Query   string
	Sources []togglableSource

	Results []searchResult
	Time    float64

	Total uint32
	Pages []ResultsPage
}

type searchResult struct {
	database.FTSResult
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
	page, err := strconv.ParseUint(req.URL.Query().Get("page"), 10, 32)

	var results []database.FTSResult
	var total *uint32
	var totalTime int64

	if len(src) > 0 && len(q) > 0 && err == nil {
		var err error
		start := time.Now().UnixMicro()

		spellchecked, err := db.Spellfix(req.Context(), q)
		if err != nil {
			slogctx.Error(req.Context(), "Error correcting query spelling", "error", err)
			w.WriteHeader(500)
			w.Write([]byte("Internal server error"))
			return
		}
		results, total, err = db.Search(req.Context(), src, spellchecked, uint32(page), 10)
		end := time.Now().UnixMicro()

		totalTime = end - start

		if err != nil || total == nil {
			slogctx.Error(req.Context(), "Error fetching results while serving results template", "error", err)
			w.WriteHeader(500)
			w.Write([]byte("Internal server error"))
			return
		}
	} else {
		results = make([]database.FTSResult, 0)
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

	ceil := math.Ceil(float64(*total) / 10.0)
	pageCount := int32(math.Min(ceil, math.MaxInt32))
	if pageCount < 1 {
		pageCount = 1
	}
	pages := CreatePagination(req.URL, int32(page), 10, pageCount)

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
			FTSResult:   res,
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
