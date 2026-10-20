package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

type Report struct {
	Country         string `json:"country"`
	WarplanesInAir  int32  `json:"warplanes_in_air"`
	WarshipsInWater int32  `json:"warships_in_water"`
	Timestamp       string `json:"timestamp"`
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
	clientURL := getenv("CLIENT_URL", "http://127.0.0.1:8082/send")
	hc := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{MaxIdleConnsPerHost: 100},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("POST /report", func(w http.ResponseWriter, r *http.Request) {
		var rep Report
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&rep); err != nil {
			http.Error(w, "json invalido", http.StatusBadRequest)
			return
		}
		if len(rep.Country) != 3 || rep.WarplanesInAir < 0 || rep.WarshipsInWater < 0 {
			http.Error(w, "datos invalidos", http.StatusBadRequest)
			return
		}
		b, _ := json.Marshal(rep)
		resp, err := hc.Post(clientURL, "application/json", bytes.NewReader(b))
		if err != nil {
			http.Error(w, "grpc-client no disponible", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})

	log.Println("api escuchando en :8081")
	log.Fatal(http.ListenAndServe(":8081", mux))
}
