package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	pb "munk8s/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type in struct {
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
	addr := getenv("GRPC_SERVER_ADDR", "grpc-server:50051")
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`))
	if err != nil {
		log.Fatalf("grpc: %v", err)
	}
	defer conn.Close()
	client := pb.NewWarReportServiceClient(conn)

	sem := make(chan struct{}, 256) // máximo de llamadas gRPC en vuelo

	mux := http.NewServeMux()
	mux.HandleFunc("POST /send", func(w http.ResponseWriter, r *http.Request) {
		var m in
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			http.Error(w, "json invalido", http.StatusBadRequest)
			return
		}
		code, ok := pb.Countries_value[strings.ToLower(m.Country)]
		if !ok || code == 0 {
			http.Error(w, "pais no aceptado", http.StatusBadRequest)
			return
		}
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		case <-r.Context().Done():
			http.Error(w, "cancelado", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		resp, err := client.SendReport(ctx, &pb.WarReportRequest{
			Country:         pb.Countries(code),
			WarplanesInAir:  m.WarplanesInAir,
			WarshipsInWater: m.WarshipsInWater,
			Timestamp:       m.Timestamp,
		})
		if err != nil {
			log.Printf("error gRPC: %v", err)
			http.Error(w, "grpc-server no disponible", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": resp.Status})
	})

	log.Printf("grpc-client -> %s, escuchando en 127.0.0.1:8082", addr)
	log.Fatal(http.ListenAndServe("127.0.0.1:8082", mux))
}
