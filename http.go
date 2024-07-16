package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

func serve(db Database, hostname string, port int16) {
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Add("X-Robots-Tag", "noindex")
		w.Write([]byte("Easysearch, an open-source project by FluxCapacitor2"))
	})

	http.HandleFunc("/search", func(w http.ResponseWriter, req *http.Request) {
		src := req.URL.Query()["source"]
		q := req.URL.Query().Get("q")
		if q != "" && src != nil && len(src) > 0 {
			results, err := db.search(src, q)
			if err != nil {
				w.Write([]byte(fmt.Sprintf("Error searching!\n\n%v", err)))
			} else {
				json, err := json.Marshal(results)
				if err == nil {
					w.Header().Add("Content-Type", "application/json")
					if len(results) == 0 {
						// By default, marshalling an empty array results in `null`. Instead, return an empty results list.
						w.Write([]byte(`{"success":"true","results":[]}`))
					} else {
						w.Write([]byte(fmt.Sprintf(`{"success":"true","results":%s}`, json)))
					}
				} else {
					w.Write([]byte(fmt.Sprintf("Error formatting JSON: %v\n", err)))
				}
			}
		} else {
			w.WriteHeader(400)
			w.Write([]byte(`{"success":"false","error": "Bad request"}`))
		}
	})


	addr := fmt.Sprintf("%v:%v", hostname, port)
	fmt.Printf("Listening on http://%v\n", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}