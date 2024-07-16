package main

import (
	"fmt"
	"net/http"
)

type httpHandler struct {
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, "Hello, world!")
}
