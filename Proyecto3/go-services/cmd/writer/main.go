package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
	queue := getenv("QUEUE", "war-reports")
	u := url.URL{
		Scheme: "amqp",
		User:   url.UserPassword(getenv("RABBIT_USER", "guest"), getenv("RABBIT_PASS", "guest")),
		Host:   getenv("RABBIT_HOST", "rabbitmq.messaging.svc.cluster.local:5672"),
		Path:   "/",
	}

	var conn *amqp.Connection
	var err error
	for i := 0; i < 30; i++ {
		if conn, err = amqp.Dial(u.String()); err == nil {
			break
		}
		log.Printf("esperando RabbitMQ (%d/30): %v", i+1, err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatal(err)
	}
	// Si se pierde la conexión, salimos y Kubernetes reinicia el container
	go func() { log.Fatalf("conexion RabbitMQ cerrada: %v", <-conn.NotifyClose(make(chan *amqp.Error, 1))) }()

	const poolSize = 8
	pool := make(chan *amqp.Channel, poolSize)
	for i := 0; i < poolSize; i++ {
		ch, err := conn.Channel()
		if err != nil {
			log.Fatal(err)
		}
		if i == 0 { // cola durable
			if _, err = ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
				log.Fatal(err)
			}
		}
		pool <- ch
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /publish", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if err != nil || !json.Valid(body) {
			http.Error(w, "json invalido", http.StatusBadRequest)
			return
		}
		var ch *amqp.Channel
		select {
		case ch = <-pool:
		case <-r.Context().Done():
			http.Error(w, "cancelado", http.StatusServiceUnavailable)
			return
		}
		defer func() { pool <- ch }()

		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		err = ch.PublishWithContext(ctx, "", queue, false, false, amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         body,
		})
		if err != nil {
			log.Printf("error publicando: %v", err)
			if ch.IsClosed() {
				log.Fatal("canal cerrado, reiniciando")
			}
			http.Error(w, "rabbitmq no disponible", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})

	log.Printf("writer -> cola %q, escuchando en 127.0.0.1:8083", queue)
	log.Fatal(http.ListenAndServe("127.0.0.1:8083", mux))
}
