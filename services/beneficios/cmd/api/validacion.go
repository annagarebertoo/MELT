package main

import (
	"encoding/json"
	"errors"
	"io"
)

// Los nombres son exactos: encoding/json sobre un struct también aceptaría
// variantes como PUNTOS. Se rechazan además claves duplicadas y campos ajenos.
type entradaAcreditacion struct {
	AcreditarRequest
	Email       string
	Descripcion string
	Referencia  string
}

func decodificarAcreditacion(body io.Reader) (entradaAcreditacion, error) {
	var req entradaAcreditacion
	dec := json.NewDecoder(body)
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return req, errors.New("se requiere un objeto JSON")
	}
	vistos := map[string]bool{}
	for dec.More() {
		token, err = dec.Token()
		if err != nil {
			return req, err
		}
		nombre, ok := token.(string)
		if !ok || vistos[nombre] {
			return req, errors.New("campo duplicado o inválido")
		}
		vistos[nombre] = true
		switch nombre {
		case "solicitudId":
			req.SolicitudID, err = decodificarTexto(dec)
		case "referencia":
			req.Referencia, err = decodificarTexto(dec)
		case "email":
			req.Email, err = decodificarTexto(dec)
		case "descripcion":
			req.Descripcion, err = decodificarTexto(dec)
		case "usuarioId":
			req.UsuarioID, err = decodificarTexto(dec)
		case "puntos":
			err = dec.Decode(&req.Puntos)
		default:
			return req, errors.New("campo desconocido")
		}
		if err != nil {
			return req, err
		}
	}
	if _, err = dec.Token(); err != nil {
		return req, err
	}
	if !vistos["puntos"] || vistos["solicitudId"] == vistos["referencia"] || vistos["usuarioId"] == vistos["email"] {
		return req, errors.New("faltan campos obligatorios")
	}
	if dec.Decode(new(any)) != io.EOF {
		return req, errors.New("se requiere un único objeto JSON")
	}
	if vistos["referencia"] {
		req.SolicitudID = req.Referencia
	}
	return req, nil
}

func decodificarTexto(dec *json.Decoder) (string, error) {
	token, err := dec.Token()
	if err != nil {
		return "", err
	}
	value, ok := token.(string)
	if !ok {
		return "", errors.New("se requiere texto")
	}
	return value, nil
}
