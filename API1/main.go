package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Definimos los moldes de los json
type HealthResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	VM        int    `json:"VM"`
	Carnet    string `json:"carnet"`
}

type CallResponse struct {
	Apiname    string `json:"apiname"`
	Message    string `json:"message"`
	Connection bool   `json:"connection"`
	Carnet     string `json:"carnet"`
}

// variables constantes
const (
	carnet   = "202302232"
	vmNumber = 1
	apiName  = "API1"
	host     = "0.0.0.0"
	port     = "8080"
)

// Handler para /health

func healthHandler(w http.ResponseWriter, r *http.Request) {

	respuesta := HealthResponse{
		Status:    "UP",
		Message:   "API1 is Ready",
		Timestamp: time.Now().Format(time.RFC3339),
		VM:        vmNumber,
		Carnet:    carnet,
	}

	//Configuración para que el header sea JSON
	w.Header().Set("Content-Type", "application/json")

	//Codificar y enviar la respuesta
	json.NewEncoder(w).Encode(respuesta)
}

// Handler para /api1/202302232/call-api2
func callAPI2Handler(w http.ResponseWriter, r *http.Request) {
	api2URL := os.Getenv("API2_URL")
	if api2URL == "" {
		api2URL = "http://localhost:8082/health"
	}

	// Hacer la llamada HTTP a API2
	resp, err := http.Get(api2URL)

	// Declaramos la variable connection (inicialmente false)
	connection := false

	// Si no hay error, procesar la respuesta
	if err == nil {
		defer resp.Body.Close()

		// Decodificar la respuesta de API2
		var health HealthResponse
		err := json.NewDecoder(resp.Body).Decode(&health)

		// Si se pudo decodificar y status es "UP", connection = true
		if err == nil && health.Status == "UP" {
			connection = true
		}
	}

	// Construir la respuesta final
	respuesta := CallResponse{
		Apiname:    "API2",
		Message:    "The API2 located on the VM1 is working",
		Connection: connection,
		Carnet:     carnet,
	}

	// Si connection es false, cambiar el mensaje
	if !connection {
		respuesta.Message = "ERROR: The API2 located on the VM1 is not working"
	}

	// Enviar la respuesta al cliente
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(respuesta)
}

// Handler para /api1/202302232/call-api3
func callAPI3Handler(w http.ResponseWriter, r *http.Request) {
	api3URL := os.Getenv("API3_URL")
	if api3URL == "" {
		api3URL = "http://localhost:8083/health" // Valor por defecto
	}

	// Misma lógica que callAPI2Handler, pero llamando a API3
	resp, err := http.Get(api3URL)

	connection := false

	if err == nil {
		defer resp.Body.Close()

		var health HealthResponse
		err := json.NewDecoder(resp.Body).Decode(&health)

		if err == nil && health.Status == "UP" {
			connection = true
		}
	}

	respuesta := CallResponse{
		Apiname:    "API3",
		Message:    "The API3 located on the VM2 is working",
		Connection: connection,
		Carnet:     carnet,
	}

	if !connection {
		respuesta.Message = "ERROR: The API3 located on the VM2 is not working"
	}

	// Enviar la respuesta al cliente
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(respuesta)
}

// Función principal ----------------------------------------------------------------------------
func main() {
	// Registrar las rutas con sus handlers
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/api1/"+carnet+"/call-api2", callAPI2Handler)
	http.HandleFunc("/api1/"+carnet+"/call-api3", callAPI3Handler)

	//Mensaje en consola para saber que está ocurriendo
	fullAddress := host + ":" + port
	fmt.Printf("Servidor %s corriendo en %s\n", apiName, fullAddress)
	fmt.Printf("Endpoints disponibles:\n")
	fmt.Printf("   GET /health\n")
	fmt.Printf("   GET /api1/%s/call-api2\n", carnet)
	fmt.Printf("   GET /api1/%s/call-api3\n", carnet)

	// Iniciar el servidor
	err := http.ListenAndServe(fullAddress, nil)
	if err != nil {
		fmt.Printf("Error al iniciar el servidor: %v\n", err)
	}
}
