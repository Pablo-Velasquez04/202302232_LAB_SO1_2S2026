package main

import (
	"encoding/json"
	"log"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
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

var total atomic.Int64

// TODO (con el Operator): escribir en Valkey
func process(r Report) error {
	if n := total.Add(1); n%100 == 1 {
		log.Printf("procesado #%d pais=%s aviones=%d barcos=%d", n, r.Country, r.WarplanesInAir, r.WarshipsInWater)
	}
	return nil
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
	ch, err := conn.Channel()
	if err != nil {
		log.Fatal(err)
	}
	if _, err = ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		log.Fatal(err)
	}
	if err = ch.Qos(50, 0, false); err != nil {
		log.Fatal(err)
	}
	msgs, err := ch.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range msgs {
				var r Report
				if err := json.Unmarshal(d.Body, &r); err != nil {
					d.Nack(false, false) // mensaje corrupto: se descarta
					continue
				}
				if err := process(r); err != nil {
					d.Nack(false, true) // reintentar
					continue
				}
				d.Ack(false)
			}
		}()
	}
	log.Printf("consumer leyendo cola %q con 4 workers", queue)
	wg.Wait()
	log.Fatal("canal de consumo cerrado")
}
