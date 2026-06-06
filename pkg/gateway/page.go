package gateway

import (
	_ "embed"
	"net/http"
)

//go:embed search.html
var searchPage string

// HandlePage serves the search UI.
func HandlePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(searchPage))
}
