//go:build ebpf

package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/redis/go-redis/v9"
)

// iniciarSondaEBPF carga el programa eBPF compilado y escucha eventos del kernel.
// Se ejecuta en una goroutine dedicada de forma concurrente al loop principal.
// Solo registra un contenedor en Valkey cuando recibe la confirmación real del kernel.
func iniciarSondaEBPF(rdb *redis.Client) {
	// Eliminar el límite de memoria bloqueada para que eBPF pueda funcionar
	if err := rlimit.RemoveMemlock(); err != nil {
		log.Printf("[eBPF] Error al remover límite de memoria: %v", err)
		return
	}

	// Cargar los objetos eBPF compilados por bpf2go (generados con 'go generate ./...')
	objs := killProbeObjects{}
	if err := loadKillProbeObjects(&objs, nil); err != nil {
		log.Printf("[eBPF] Error al cargar objetos: %v", err)
		log.Printf("[eBPF] ¿Ejecutaste 'go generate ./...' con clang instalado?")
		return
	}
	defer objs.Close()

	// Adjuntar al tracepoint del kernel: syscalls/sys_enter_kill
	tp, err := link.Tracepoint("syscalls", "sys_enter_kill", objs.TraceSysEnterKill, nil)
	if err != nil {
		log.Printf("[eBPF] Error al adjuntar tracepoint sys_enter_kill: %v", err)
		return
	}
	defer tp.Close()
	fmt.Println("[eBPF] Sonda activa en syscalls/sys_enter_kill. Escuchando eventos...")

	// Crear lector del RingBuffer (mapa eBPF definido en kill_probe.c)
	rd, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		log.Printf("[eBPF] Error al crear lector de RingBuffer: %v", err)
		return
	}
	defer rd.Close()

	// Bucle de lectura de eventos del kernel
	for {
		record, err := rd.Read()
		if err != nil {
			if strings.Contains(err.Error(), "closed") {
				return
			}
			log.Printf("[eBPF] Error leyendo evento: %v", err)
			continue
		}

		// El evento debe tener exactamente 12 bytes: 3 campos de uint32/int32
		if len(record.RawSample) < 12 {
			continue
		}

		// Parsear los bytes en little-endian (igual que la struct kill_event en C)
		senderPid := uint32(record.RawSample[0]) | uint32(record.RawSample[1])<<8 |
			uint32(record.RawSample[2])<<16 | uint32(record.RawSample[3])<<24
		targetPid := uint32(record.RawSample[4]) | uint32(record.RawSample[5])<<8 |
			uint32(record.RawSample[6])<<16 | uint32(record.RawSample[7])<<24
		signal := int32(record.RawSample[8]) | int32(record.RawSample[9])<<8 |
			int32(record.RawSample[10])<<16 | int32(record.RawSample[11])<<24

		sigNombre := "SIGTERM"
		if signal == 9 {
			sigNombre = "SIGKILL"
		}

		fmt.Printf("[eBPF] EVENTO CAPTURADO - %s (%d) | Emisor PID: %d -> Receptor PID: %d\n",
			sigNombre, signal, senderPid, targetPid)

		// Verificar si el PID receptor corresponde a uno de nuestros contenedores
		dockerMapMu.RLock()
		dInfo, esContenedor := dockerMapGlobal[int(targetPid)]
		dockerMapMu.RUnlock()

		if esContenedor {
			fmt.Printf("[eBPF] ✓ CONFIRMACION KERNEL: '%s' (ID: %s) eliminado con %s\n",
				dInfo.Name, dInfo.ID, sigNombre)

			// Registrar en Valkey SOLO aquí: confirmación real a nivel de kernel
			if rdb != nil {
				rdb.XAdd(context.Background(), &redis.XAddArgs{
					Stream: "killed_history",
					Values: map[string]interface{}{
						"container_id":   dInfo.ID,
						"container_name": dInfo.Name,
						"target_pid":     int(targetPid),
						"sender_pid":     int(senderPid),
						"signal":         sigNombre,
						"timestamp":      time.Now().Format(time.RFC3339),
					},
				})
				fmt.Printf("[eBPF] -> Eliminación registrada en Valkey (stream: killed_history).\n")
			}
		}
	}
}
