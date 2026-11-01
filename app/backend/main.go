package main

import (
	"fmt"
	"log"
	"net/http"
)

// corsMiddleware adds CORS headers to allow requests from file:// and any origin.
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next(w, r)
	}
}

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Infrastructure Insight")
	})

	http.HandleFunc("/metrics", corsMiddleware(metricsHandler))
	http.HandleFunc("/api/metrics", corsMiddleware(metricsHandler))

	addr := ":5000"
	fmt.Println("Server running at http://localhost:5000")

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal("Server failed: ", err)
	}
}
