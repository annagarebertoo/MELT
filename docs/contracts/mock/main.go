package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

type Tratamiento struct {
	ID              int      `json:"id"`
	Nombre          string   `json:"nombre"`
	Categoria       string   `json:"categoria"`
	Descripcion     string   `json:"descripcion"`
	Precio          float64  `json:"precio"`
	Moneda          string   `json:"moneda"`
	DuracionMinutos int      `json:"duracionMinutos"`
	Imagenes        []string `json:"imagenes"`
}

type Pagination struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"totalPages"`
}

type TratamientosResponse struct {
	Tratamientos []Tratamiento `json:"tratamientos"`
	Pagination   Pagination    `json:"pagination"`
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

var tratamientos = []Tratamiento{
	{
		ID:              1,
		Nombre:          "Limpieza facial profunda",
		Categoria:       "Facial",
		Descripcion:     "Limpieza profunda de la piel.",
		Precio:          15000,
		Moneda:          "ARS",
		DuracionMinutos: 60,
		Imagenes: []string{
			"https://melt.example.com/images/limpieza-facial.jpg",
		},
	},
	{
		ID:              2,
		Nombre:          "Limpieza facial express",
		Categoria:       "Facial",
		Descripcion:     "Tratamiento facial de limpieza rápida.",
		Precio:          10000,
		Moneda:          "ARS",
		DuracionMinutos: 30,
		Imagenes: []string{
			"https://melt.example.com/images/limpieza-express.jpg",
		},
	},
	{
		ID:              3,
		Nombre:          "Masaje relajante",
		Categoria:       "Corporal",
		Descripcion:     "Masaje corporal para relajación.",
		Precio:          18000,
		Moneda:          "ARS",
		DuracionMinutos: 60,
		Imagenes: []string{
			"https://melt.example.com/images/masaje-relajante.jpg",
		},
	},
	{
		ID:              4,
		Nombre:          "Depilación definitiva",
		Categoria:       "Depilación",
		Descripcion:     "Tratamiento de depilación definitiva.",
		Precio:          22000,
		Moneda:          "ARS",
		DuracionMinutos: 45,
		Imagenes: []string{
			"https://melt.example.com/images/depilacion-definitiva.jpg",
		},
	},
	{
		ID:              5,
		Nombre:          "Manicura clásica",
		Categoria:       "Manos y uñas",
		Descripcion:     "Cuidado y esmaltado de uñas.",
		Precio:          9000,
		Moneda:          "ARS",
		DuracionMinutos: 45,
		Imagenes: []string{
			"https://melt.example.com/images/manicura.jpg",
		},
	},
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/tratamientos", tratamientosHandler)
	mux.HandleFunc("/api/v1/tratamientos/", tratamientoPorIDHandler)

	fmt.Println("Mock de tratamientos escuchando en http://localhost:8080")

	err := http.ListenAndServe(":8080", mux)
	if err != nil {
		fmt.Println("Error al iniciar el servidor:", err)
	}
}

func tratamientosHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Método no permitido")
		return
	}

	query := r.URL.Query()

	q := strings.TrimSpace(query.Get("q"))
	categoria := strings.TrimSpace(query.Get("categoria"))
	precioMaxStr := strings.TrimSpace(query.Get("precioMax"))

	page, err := parsePositiveInt(query.Get("page"), 1)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PARAMETER", "El parámetro page debe ser un número entero mayor o igual a 1")
		return
	}

	limit, err := parsePositiveInt(query.Get("limit"), 10)
	if err != nil || limit > 50 {
		writeError(w, http.StatusBadRequest, "INVALID_PARAMETER", "El parámetro limit debe ser un número entre 1 y 50")
		return
	}

	sortField := query.Get("sort")
	if sortField == "" {
		sortField = "nombre"
	}

	if sortField != "nombre" && sortField != "precio" {
		writeError(w, http.StatusBadRequest, "INVALID_PARAMETER", "El parámetro sort debe ser nombre o precio")
		return
	}

	order := query.Get("order")
	if order == "" {
		order = "asc"
	}

	if order != "asc" && order != "desc" {
		writeError(w, http.StatusBadRequest, "INVALID_PARAMETER", "El parámetro order debe ser asc o desc")
		return
	}

	var precioMax float64

	if precioMaxStr != "" {
		precioMax, err = strconv.ParseFloat(precioMaxStr, 64)
		if err != nil || precioMax < 0 {
			writeError(w, http.StatusBadRequest, "INVALID_PARAMETER", "El parámetro precioMax debe ser un número mayor o igual a 0")
			return
		}
	}

	resultados := filtrarTratamientos(q, categoria, precioMaxStr, precioMax)

	ordenarTratamientos(resultados, sortField, order)

	total := len(resultados)
	totalPages := 0

	if total > 0 {
		totalPages = (total + limit - 1) / limit
	}

	if page > totalPages && total > 0 {
		writeError(w, http.StatusBadRequest, "INVALID_PARAMETER", "La página solicitada no existe")
		return
	}

	inicio := (page - 1) * limit
	fin := inicio + limit

	if inicio > total {
		inicio = total
	}

	if fin > total {
		fin = total
	}

	paginaResultados := resultados[inicio:fin]

	response := TratamientosResponse{
		Tratamientos: paginaResultados,
		Pagination: Pagination{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	}

	writeJSON(w, http.StatusOK, response)
}

func tratamientoPorIDHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Método no permitido")
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/tratamientos/")

	id, err := strconv.Atoi(idStr)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_PARAMETER", "El identificador debe ser un número entero positivo")
		return
	}

	for _, tratamiento := range tratamientos {
		if tratamiento.ID == id {
			writeJSON(w, http.StatusOK, tratamiento)
			return
		}
	}

	writeError(
		w,
		http.StatusNotFound,
		"TRATAMIENTO_NOT_FOUND",
		"No existe un tratamiento con el identificador solicitado",
	)
}

func filtrarTratamientos(q, categoria, precioMaxStr string, precioMax float64) []Tratamiento {
	var resultados []Tratamiento

	for _, tratamiento := range tratamientos {

		if q != "" {
			nombre := strings.ToLower(tratamiento.Nombre)
			busqueda := strings.ToLower(q)

			if !strings.Contains(nombre, busqueda) {
				continue
			}
		}

		if categoria != "" && tratamiento.Categoria != categoria {
			continue
		}

		if precioMaxStr != "" && tratamiento.Precio > precioMax {
			continue
		}

		resultados = append(resultados, tratamiento)
	}

	return resultados
}

func ordenarTratamientos(resultados []Tratamiento, sortField, order string) {
	sort.Slice(resultados, func(i, j int) bool {
		if sortField == "precio" {
			if resultados[i].Precio != resultados[j].Precio {
				if order == "desc" {
					return resultados[i].Precio > resultados[j].Precio
				}
				return resultados[i].Precio < resultados[j].Precio
			}
		}

		nombreI := strings.ToLower(resultados[i].Nombre)
		nombreJ := strings.ToLower(resultados[j].Nombre)

		if order == "desc" {
			return nombreI > nombreJ
		}

		return nombreI < nombreJ
	})
}

func parsePositiveInt(value string, defaultValue int) (int, error) {
	if value == "" {
		return defaultValue, nil
	}

	number, err := strconv.Atoi(value)

	if err != nil || number < 1 {
		return 0, fmt.Errorf("valor inválido")
	}

	return number, nil
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	response := ErrorResponse{
		Error: ErrorDetail{
			Code:    code,
			Message: message,
		},
	}

	writeJSON(w, status, response)
}