package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	pb "munk8s/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type server struct {
	pb.UnimplementedWarReportServiceServer
	n         atomic.Int64
	hc        *http.Client
	writerURL string
}

func (s *server) SendReport(ctx context.Context, r *pb.WarReportRequest) (*pb.WarReportResponse, error) {
	if n := s.n.Add(1); n%100 == 1 {
		log.Printf("reporte #%d pais=%s aviones=%d barcos=%d", n, r.Country, r.WarplanesInAir, r.WarshipsInWater)
	}
	b, _ := json.Marshal(map[string]any{
		"country":           strings.ToUpper(r.Country.String()),
		"warplanes_in_air":  r.WarplanesInAir,
		"warships_in_water": r.WarshipsInWater,
		"timestamp":         r.Timestamp,
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.writerURL, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "writer no disponible")
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return nil, status.Error(codes.Unavailable, "writer rechazo el mensaje")
	}
	return &pb.WarReportResponse{Status: "ok"}, nil
}

func main() {
	wurl := os.Getenv("WRITER_URL")
	if wurl == "" {
		wurl = "http://127.0.0.1:8083/publish"
	}
	s := &server{
		hc: &http.Client{Timeout: 3 * time.Second,
			Transport: &http.Transport{MaxIdleConnsPerHost: 100}},
		writerURL: wurl,
	}
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatal(err)
	}
	g := grpc.NewServer()
	pb.RegisterWarReportServiceServer(g, s)
	log.Println("grpc-server escuchando en :50051")
	log.Fatal(g.Serve(lis))
}
