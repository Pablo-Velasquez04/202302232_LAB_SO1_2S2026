package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
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

func main() {
	fmt.Println("========== Iniciando Daemon (Fase 1, 2 y 3) ==========")

	// 1. Conexión a Docker
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		fmt.Printf("Error al crear cliente Docker: %v\n", err)
		return
	}

	// 2. Conexión a Valkey/Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379", // Si Valkey está en docker, mapea el puerto 6379 o usa su IP
		Password: "",               // Sin contraseña por defecto
		DB:       0,
	})

	_, err = rdb.Ping(context.Background()).Result()
	if err != nil {
		fmt.Printf("Advertencia: No se pudo conectar a Valkey en localhost:6379: %v\n", err)
		fmt.Println("Asegúrate de tener Valkey ejecutándose (docker run -p 6379:6379 -d valkey/valkey).")
	} else {
		fmt.Println("Conexión exitosa a Valkey.")
	}

	// 3. Iniciar Loop Principal (20 segundos)
	fmt.Println("Iniciando loop principal (cada 20 segundos)...")
	ticker := time.NewTicker(20 * time.Second)

	// Ejecutar la primera iteración inmediatamente
	ejecutarIteracion(cli, rdb)

	for range ticker.C {
		ejecutarIteracion(cli, rdb)
	}
}

func ejecutarIteracion(cli *client.Client, rdb *redis.Client) {
	fmt.Printf("\n--- Ejecutando análisis (%s) ---\n", time.Now().Format("15:04:05"))

	dockerMap, err := mapearContenedores(cli)
	if err != nil {
		fmt.Printf("Error al conectar con Docker: %v\n", err)
		return
	}

	contenedores, sysMetrics, err := leerYFiltrarProc(procFilePath, dockerMap)
	if err != nil {
		fmt.Printf("Error al leer /proc: %v\n", err)
		return
	}

	fmt.Printf("RAM Total: %d MB | RAM Usada: %d MB | RAM Libre: %d MB\n", sysMetrics.TotalRAM, sysMetrics.UsedRAM, sysMetrics.FreeRAM)

	kills := gestionarContenedores(cli, contenedores)

	if rdb != nil {
		guardarEnValkey(rdb, sysMetrics, contenedores, kills)
		fmt.Println("-> Métricas almacenadas en Valkey exitosamente.")
	}
}

// mapearContenedores devuelve un diccionario donde la llave es el PID y el valor es la info de Docker
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

// leerYFiltrarProc lee el archivo del kernel y extrae métricas de contenedores y sistema
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

		// Leer métricas globales del sistema
		if strings.HasPrefix(linea, "Total RAM:") {
			fields := strings.Fields(linea)
			if len(fields) >= 3 {
				val, _ := strconv.ParseUint(fields[2], 10, 64)
				sysMetrics.TotalRAM = val
			}
			continue
		}
		if strings.HasPrefix(linea, "Free RAM:") {
			fields := strings.Fields(linea)
			if len(fields) >= 3 {
				val, _ := strconv.ParseUint(fields[2], 10, 64)
				sysMetrics.FreeRAM = val
			}
			continue
		}
		if strings.HasPrefix(linea, "Used RAM:") {
			fields := strings.Fields(linea)
			if len(fields) >= 3 {
				val, _ := strconv.ParseUint(fields[2], 10, 64)
				sysMetrics.UsedRAM = val
			}
			continue
		}

		// Detectar inicio de tabla de procesos
		if strings.Contains(linea, "PID\tNombre") {
			leyendoProcesos = true
			continue
		}

		if leyendoProcesos {
			columnas := strings.Fields(linea)
			if len(columnas) >= 6 {
				pidStr := columnas[0]
				pid, _ := strconv.Atoi(pidStr)

				if dInfo, existe := dockerMap[pid]; existe {
					vsz, _ := strconv.Atoi(columnas[2])
					rss, _ := strconv.Atoi(columnas[3])
					memPercent, _ := strconv.Atoi(columnas[4])
					cpuVal, _ := strconv.Atoi(columnas[5])

					datos := ContenedorData{
						ID:         dInfo.ID,
						Name:       dInfo.Name,
						Image:      dInfo.Image,
						PID:        pid,
						VSZ:        vsz,
						RSS:        rss,
						MemPercent: memPercent,
						CPU:        cpuVal,
					}
					contenedoresFiltrados = append(contenedoresFiltrados, datos)
				}
			}
		}
	}

	return contenedoresFiltrados, sysMetrics, scanner.Err()
}

// gestionarContenedores aplica el algoritmo de selección y eliminación según reglas. Devuelve contenedores matados.
func gestionarContenedores(cli *client.Client, contenedores []ContenedorData) int {
	var bajoConsumo []ContenedorData
	var altoConsumo []ContenedorData
	var intrusos []ContenedorData

	for _, c := range contenedores {
		img := strings.ToLower(c.Image)
		name := strings.ToLower(c.Name)

		if strings.Contains(img, "bajo-consumo") || strings.Contains(name, "bajo-consumo") {
			bajoConsumo = append(bajoConsumo, c)
		} else if strings.Contains(img, "alto-cpu") || strings.Contains(img, "alto-ram") || strings.Contains(img, "go-client") || strings.Contains(name, "alto-cpu") || strings.Contains(name, "alto-ram") || strings.Contains(name, "go-client") {
			altoConsumo = append(altoConsumo, c)
		} else if strings.Contains(img, "intruso") || strings.Contains(name, "intruso") {
			intrusos = append(intrusos, c)
		}
	}

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

	kills := 0
	matar := func(c ContenedorData, razon string) {
		fmt.Printf("-> Matando contenedor %s (ID: %s) - Razón: %s\n", c.Name, c.ID, razon)
		err := cli.ContainerKill(context.Background(), c.ID, "SIGKILL")
		if err != nil {
			fmt.Printf("   Error matando %s: %v\n", c.ID, err)
		} else {
			fmt.Printf("   Contenedor %s terminado exitosamente.\n", c.ID)
			kills++
		}
	}

	if len(bajoConsumo) > 3 {
		for i := 0; i < len(bajoConsumo)-3; i++ {
			matar(bajoConsumo[i], "Exceso de bajo consumo (optimización de recursos)")
		}
	}

	if len(altoConsumo) > 2 {
		for i := 0; i < len(altoConsumo)-2; i++ {
			matar(altoConsumo[i], "Exceso de alto consumo (optimización de recursos)")
		}
	}

	for _, c := range intrusos {
		matar(c, "Contenedor intruso detectado")
	}

	return kills
}

// guardarEnValkey persiste todas las métricas en Redis/Valkey para Grafana
func guardarEnValkey(rdb *redis.Client, sysMetrics SystemMetrics, contenedores []ContenedorData, kills int) {
	ctx := context.Background()

	// 1. Visión General (Datos en Texto para Tarjetas de Grafana)
	rdb.Set(ctx, "total_ram", fmt.Sprintf("%d MB", sysMetrics.TotalRAM), 0)
	rdb.Set(ctx, "used_ram", fmt.Sprintf("%d MB", sysMetrics.UsedRAM), 0)
	rdb.Set(ctx, "free_ram", fmt.Sprintf("%d MB", sysMetrics.FreeRAM), 0)

	// 2. Evolución Temporal (Streams para series de tiempo)
	rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: "ram_history",
		Values: map[string]interface{}{
			"used_ram": sysMetrics.UsedRAM,
			"free_ram": sysMetrics.FreeRAM,
		},
	})

	// Registrar siempre, incluso si es 0, para mantener continuidad en la gráfica
	rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: "killed_history",
		Values: map[string]interface{}{
			"killed_count": kills,
		},
	})

	// 3. Top Rankings
	// Ordenar todos los contenedores por RAM (descendente)
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

	// Ordenar todos los contenedores por CPU (descendente)
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
