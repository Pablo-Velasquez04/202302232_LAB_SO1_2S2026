package main

import (
	"context"
	"log"
	"net"
	"sync/atomic"

	pb "munk8s/proto"

	"google.golang.org/grpc"
)

type server struct {
	pb.UnimplementedWarReportServiceServer
	n atomic.Int64
}

func (s *server) SendReport(ctx context.Context, r *pb.WarReportRequest) (*pb.WarReportResponse, error) {
	// Se loguea 1 de cada 100 para no inundar los logs
	if n := s.n.Add(1); n%100 == 1 {
		log.Printf("reporte #%d pais=%s aviones=%d barcos=%d",
			n, r.Country, r.WarplanesInAir, r.WarshipsInWater)
	}
	// SIGUIENTE BLOQUE: aquí se reenvía al container Writer (RabbitMQ)
	return &pb.WarReportResponse{Status: "ok"}, nil
}

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatal(err)
	}
	g := grpc.NewServer()
	pb.RegisterWarReportServiceServer(g, &server{})
	log.Println("grpc-server escuchando en :50051")
	log.Fatal(g.Serve(lis))
}
