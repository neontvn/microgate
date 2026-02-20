package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
)

func main() {
	port := flag.Int("port", 9001, "port to run the test backend on")
	flag.Parse()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[backend:%d] %s %s", *port, r.Method, r.URL.Path)

		w.Header().Set("Content-Type", "application/json")

		resp := map[string]interface{}{
			"message": "Hello from backend",
			"port":    *port,
			"path":    r.URL.Path,
		}

		json.NewEncoder(w).Encode(resp)
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Test backend starting on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
