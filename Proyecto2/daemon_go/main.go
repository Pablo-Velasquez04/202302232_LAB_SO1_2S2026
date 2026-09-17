package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/redis/go-redis/v9"
)

// Ruta del archivo generado por tu módulo en C
const procFilePath = "/proc/continfo_pr2_so1_202302232"

// Estructuras de datos
type DockerInfo struct {
	ID    string
	Name  string
	Image string
}

type ContenedorData struct {
	ID         string
	Name       string
	Image      string
	PID        int
	VSZ        int
	RSS        int
	MemPercent int
	CPU        int
}

type SystemMetrics struct {
	TotalRAM uint64
	FreeRAM  uint64
	UsedRAM  uint64
}

// Mapa global compartido entre el loop principal y la goroutine eBPF.
// Protegido por un mutex para acceso concurrente seguro.
var (
	dockerMapMu     sync.RWMutex
	dockerMapGlobal map[int]DockerInfo
)

func main() {
	fmt.Println("========== Iniciando Daemon (Fase 1, 2, 3 + eBPF) ==========")

	// --- Conexión a Docker ---
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("Error al crear cliente Docker: %v", err)
	}

	// --- Conexión a Valkey/Redis ---
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})

	if _, err = rdb.Ping(context.Background()).Result(); err != nil {
		fmt.Printf("Advertencia: No se pudo conectar a Valkey: %v\n", err)
		rdb = nil
	} else {
		fmt.Println("Conexión exitosa a Valkey.")
	}

	// --- Inicializar mapa Docker global ---
	dockerMapGlobal, _ = mapearContenedores(cli)

	// --- Cargar y ejecutar la sonda eBPF en una goroutine dedicada ---
	// La función iniciarSondaEBPF está definida en ebpf_probe.go
	go iniciarSondaEBPF(rdb)

	// --- Manejar señales para cierre limpio (Ctrl+C) ---
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	// --- Loop Principal (cada 20 segundos) ---
	fmt.Println("Iniciando loop principal (cada 20 segundos)...")
	ticker := time.NewTicker(20 * time.Second)

	// Primera iteración inmediata
	ejecutarIteracion(cli, rdb)

	for {
		select {
		case <-ticker.C:
			ejecutarIteracion(cli, rdb)
		case <-sig:
			fmt.Println("\nSeñal de cierre recibida. Apagando Daemon...")
			ticker.Stop()
			return
		}
	}
}

func ejecutarIteracion(cli *client.Client, rdb *redis.Client) {
	fmt.Printf("\n--- Ejecutando análisis (%s) ---\n", time.Now().Format("15:04:05"))

	// Actualizar mapa Docker global (protegido con mutex)
	nuevoDM, err := mapearContenedores(cli)
	if err != nil {
		fmt.Printf("Error al conectar con Docker: %v\n", err)
		return
	}
	dockerMapMu.Lock()
	dockerMapGlobal = nuevoDM
	dockerMapMu.Unlock()

	contenedores, sysMetrics, err := leerYFiltrarProc(procFilePath, nuevoDM)
	if err != nil {
		fmt.Printf("Error al leer /proc: %v\n", err)
		return
	}

	fmt.Printf("RAM Total: %d MB | RAM Usada: %d MB | RAM Libre: %d MB\n",
		sysMetrics.TotalRAM, sysMetrics.UsedRAM, sysMetrics.FreeRAM)
	fmt.Printf("Contenedores detectados: %d\n", len(contenedores))
	for _, c := range contenedores {
		fmt.Printf("  -> ID: %s | %s | PID: %d | RSS: %d KB | CPU: %d\n",
			c.ID, c.Name, c.PID, c.RSS, c.CPU)
	}

	// Gestionar contenedores (matar excedentes e intrusos).
	// NOTA: El registro en Valkey de "eliminados" ya NO ocurre aquí,
	// sino en la goroutine eBPF (ebpf_probe.go) al recibir confirmación del kernel.
	gestionarContenedores(cli, contenedores)

	// Guardar métricas generales de RAM y rankings
	if rdb != nil {
		guardarMetricasGenerales(rdb, sysMetrics, contenedores)
		fmt.Println("-> Métricas generales almacenadas en Valkey exitosamente.")
	}
}

// mapearContenedores devuelve un diccionario PID -> DockerInfo para todos los contenedores activos
func mapearContenedores(cli *client.Client) (map[int]DockerInfo, error) {
	lista, err := cli.ContainerList(context.Background(), container.ListOptions{})
	if err != nil {
		return nil, err
	}

	mapa := make(map[int]DockerInfo)
	for _, cont := range lista {
		info, err := cli.ContainerInspect(context.Background(), cont.ID)
		if err == nil && info.State != nil {
			idCorto := cont.ID[:12]
			mapa[info.State.Pid] = DockerInfo{
				ID:    idCorto,
				Name:  strings.TrimPrefix(info.Name, "/"),
				Image: info.Config.Image,
			}
		}
	}
	return mapa, nil
}

// leerYFiltrarProc lee el archivo del kernel y extrae métricas del sistema y de contenedores
func leerYFiltrarProc(ruta string, dockerMap map[int]DockerInfo) ([]ContenedorData, SystemMetrics, error) {
	archivo, err := os.Open(ruta)
	if err != nil {
		return nil, SystemMetrics{}, err
	}
	defer archivo.Close()

	var contenedoresFiltrados []ContenedorData
	var sysMetrics SystemMetrics
	scanner := bufio.NewScanner(archivo)
	leyendoProcesos := false

	for scanner.Scan() {
		linea := strings.TrimSpace(scanner.Text())
		if linea == "" {
			continue
		}

		if strings.HasPrefix(linea, "Total RAM:") {
			if fields := strings.Fields(linea); len(fields) >= 3 {
				val, _ := strconv.ParseUint(fields[2], 10, 64)
				sysMetrics.TotalRAM = val
			}
			continue
		}
		if strings.HasPrefix(linea, "Free RAM:") {
			if fields := strings.Fields(linea); len(fields) >= 3 {
				val, _ := strconv.ParseUint(fields[2], 10, 64)
				sysMetrics.FreeRAM = val
			}
			continue
		}
		if strings.HasPrefix(linea, "Used RAM:") {
			if fields := strings.Fields(linea); len(fields) >= 3 {
				val, _ := strconv.ParseUint(fields[2], 10, 64)
				sysMetrics.UsedRAM = val
			}
			continue
		}

		if strings.Contains(linea, "PID\tNombre") {
			leyendoProcesos = true
			continue
		}

		if leyendoProcesos {
			columnas := strings.Fields(linea)
			if len(columnas) >= 6 {
				pid, _ := strconv.Atoi(columnas[0])
				if dInfo, existe := dockerMap[pid]; existe {
					vsz, _ := strconv.Atoi(columnas[2])
					rss, _ := strconv.Atoi(columnas[3])
					memPercent, _ := strconv.Atoi(columnas[4])
					cpuVal, _ := strconv.Atoi(columnas[5])
					contenedoresFiltrados = append(contenedoresFiltrados, ContenedorData{
						ID: dInfo.ID, Name: dInfo.Name, Image: dInfo.Image,
						PID: pid, VSZ: vsz, RSS: rss, MemPercent: memPercent, CPU: cpuVal,
					})
				}
			}
		}
	}
	return contenedoresFiltrados, sysMetrics, scanner.Err()
}

// gestionarContenedores aplica el algoritmo de selección y eliminación según las reglas del proyecto
func gestionarContenedores(cli *client.Client, contenedores []ContenedorData) {
	var bajoConsumo, altoConsumo, intrusos []ContenedorData

	for _, c := range contenedores {
		img := strings.ToLower(c.Image)
		name := strings.ToLower(c.Name)
		switch {
		case strings.Contains(img, "bajo-consumo") || strings.Contains(name, "bajo-consumo"):
			bajoConsumo = append(bajoConsumo, c)
		case strings.Contains(img, "alto-cpu") || strings.Contains(img, "alto-ram") || strings.Contains(img, "go-client") ||
			strings.Contains(name, "alto-cpu") || strings.Contains(name, "alto-ram") || strings.Contains(name, "go-client"):
			altoConsumo = append(altoConsumo, c)
		case strings.Contains(img, "intruso") || strings.Contains(name, "intruso"):
			intrusos = append(intrusos, c)
		}
	}

	// Ordenar: mayor consumo primero (MemPercent > RSS > VSZ > CPU)
	ordenar := func(lista []ContenedorData) {
		sort.Slice(lista, func(i, j int) bool {
			if lista[i].MemPercent != lista[j].MemPercent {
				return lista[i].MemPercent > lista[j].MemPercent
			}
			if lista[i].RSS != lista[j].RSS {
				return lista[i].RSS > lista[j].RSS
			}
			if lista[i].VSZ != lista[j].VSZ {
				return lista[i].VSZ > lista[j].VSZ
			}
			return lista[i].CPU > lista[j].CPU
		})
	}
	ordenar(bajoConsumo)
	ordenar(altoConsumo)

	matar := func(c ContenedorData, razon string) {
		fmt.Printf("-> Matando contenedor %s (ID: %s) - Razón: %s\n", c.Name, c.ID, razon)
		if err := cli.ContainerKill(context.Background(), c.ID, "SIGKILL"); err != nil {
			fmt.Printf("   Error matando %s: %v\n", c.ID, err)
		} else {
			fmt.Printf("   SIGKILL enviado a %s. Esperando confirmación del kernel vía eBPF...\n", c.ID)
		}
	}

	// Regla: máximo 3 contenedores de bajo consumo
	for i := 0; i < len(bajoConsumo)-3; i++ {
		matar(bajoConsumo[i], "Exceso de bajo consumo")
	}
	// Regla: máximo 2 contenedores de alto consumo
	for i := 0; i < len(altoConsumo)-2; i++ {
		matar(altoConsumo[i], "Exceso de alto consumo")
	}
	// Regla: eliminar todos los intrusos
	for _, c := range intrusos {
		matar(c, "Contenedor intruso detectado")
	}
}

// guardarMetricasGenerales persiste RAM y rankings en Valkey.
// El registro de contenedores eliminados ("killed_history") lo maneja ebpf_probe.go.
func guardarMetricasGenerales(rdb *redis.Client, sysMetrics SystemMetrics, contenedores []ContenedorData) {
	ctx := context.Background()

	// Tarjetas (valores actuales)
	rdb.Set(ctx, "total_ram", sysMetrics.TotalRAM, 0)
	rdb.Set(ctx, "used_ram", sysMetrics.UsedRAM, 0)
	rdb.Set(ctx, "free_ram", sysMetrics.FreeRAM, 0)
	rdb.Set(ctx, "total_containers", len(contenedores), 0)

	// Serie de tiempo de RAM
	rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: "ram_history",
		Values: map[string]interface{}{
			"used_ram": sysMetrics.UsedRAM,
			"free_ram": sysMetrics.FreeRAM,
		},
	})

	// Top 5 por RSS (RAM física)
	sort.Slice(contenedores, func(i, j int) bool {
		if contenedores[i].MemPercent != contenedores[j].MemPercent {
			return contenedores[i].MemPercent > contenedores[j].MemPercent
		}
		return contenedores[i].RSS > contenedores[j].RSS
	})
	rdb.Del(ctx, "top_ram")
	for i, c := range contenedores {
		if i >= 5 {
			break
		}
		rdb.ZAdd(ctx, "top_ram", redis.Z{
			Score:  float64(c.RSS),
			Member: fmt.Sprintf("%s (PID: %d)", c.Name, c.PID),
		})
	}

	// Top 5 por CPU
	sort.Slice(contenedores, func(i, j int) bool {
		return contenedores[i].CPU > contenedores[j].CPU
	})
	rdb.Del(ctx, "top_cpu")
	for i, c := range contenedores {
		if i >= 5 {
			break
		}
		rdb.ZAdd(ctx, "top_cpu", redis.Z{
			Score:  float64(c.CPU),
			Member: fmt.Sprintf("%s (PID: %d)", c.Name, c.PID),
		})
	}
}
