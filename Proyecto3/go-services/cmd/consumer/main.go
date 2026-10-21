package main

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
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

// Todo el update de un reporte es atómico (Valkey ejecuta el script sin intercalar otros comandos)
const luaScript = `
local p = tonumber(ARGV[1])
local s = tonumber(ARGV[2])
local c = ARGV[3]
local function upd(field, v, isMax)
  local cur = redis.call('HGET', KEYS[1], field)
  if (not cur) or (isMax and v > tonumber(cur)) or ((not isMax) and v < tonumber(cur)) then
    redis.call('HSET', KEYS[1], field, v)
  end
end
upd('max_planes', p, true)
upd('min_planes', p, false)
upd('max_ships', s, true)
upd('min_ships', s, false)
redis.call('ZINCRBY', KEYS[2], 1, p)
redis.call('ZINCRBY', KEYS[3], 1, s)
redis.call('HSET', KEYS[1], 'mode_planes', redis.call('ZREVRANGE', KEYS[2], 0, 0)[1])
redis.call('HSET', KEYS[1], 'mode_ships', redis.call('ZREVRANGE', KEYS[3], 0, 0)[1])
redis.call('ZINCRBY', KEYS[4], p, c)
redis.call('ZINCRBY', KEYS[5], s, c)
redis.call('HINCRBY', KEYS[6], c, 1)
redis.call('XADD', KEYS[7], 'MAXLEN', '~', 5000, '*', 'planes', p, 'ships', s, 'ts', ARGV[4])
redis.call('EXPIRE', KEYS[7], 86400)
return 1
`

var (
	script = redis.NewScript(luaScript)
	rdb    *redis.Client
	total  atomic.Int64
)

func process(r Report) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := script.Run(ctx, rdb,
		[]string{"stats", "freq:planes", "freq:ships", "top:planes", "top:ships", "reports:count", "series:" + r.Country},
		r.WarplanesInAir, r.WarshipsInWater, r.Country, r.Timestamp).Err()
	if err != nil {
		log.Printf("valkey: %v", err)
		time.Sleep(200 * time.Millisecond) // evita un bucle de reintentos a toda velocidad
		return err
	}
	if n := total.Add(1); n%500 == 1 {
		log.Printf("guardado #%d pais=%s aviones=%d barcos=%d", n, r.Country, r.WarplanesInAir, r.WarshipsInWater)
	}
	return nil
}

func main() {
	rdb = redis.NewClient(&redis.Options{
		Addr:     getenv("VALKEY_ADDR", "valkey.data.svc.cluster.local:6379"),
		PoolSize: 16,
	})

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
					d.Nack(false, true) // Valkey falló: se devuelve a la cola
					continue
				}
				d.Ack(false)
			}
		}()
	}
	log.Printf("consumer leyendo %q -> Valkey, 4 workers", queue)
	wg.Wait()
	log.Fatal("canal de consumo cerrado")
}
