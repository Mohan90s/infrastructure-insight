package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Infrastructure Insight")
	})

	http.HandleFunc("/metrics", metricsHandler)

	addr := ":5000"
	fmt.Println("Server running at http://localhost:5000")

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal("Server failed: ", err)
	}
}
