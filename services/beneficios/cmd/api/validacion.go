package main

import (
	"encoding/json"
	"errors"
	"io"
)

// Los nombres son exactos: encoding/json sobre un struct también aceptaría
// variantes como PUNTOS. Se rechazan además claves duplicadas y campos ajenos.
func decodificarAcreditacion(body io.Reader) (AcreditarRequest, error) {
	var req AcreditarRequest
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
			err = dec.Decode(&req.SolicitudID)
		case "usuarioId":
			err = dec.Decode(&req.UsuarioID)
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
	if len(vistos) != 3 {
		return req, errors.New("faltan campos obligatorios")
	}
	if dec.Decode(new(any)) != io.EOF {
		return req, errors.New("se requiere un único objeto JSON")
	}
	return req, nil
}
