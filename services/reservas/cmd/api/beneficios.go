package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
)

type canjeReservado struct {
	ID          string  `json:"id"`
	Descuento   float64 `json:"descuento"`
	PrecioFinal float64 `json:"precioFinal"`
}

func (s *servidorReservas) solicitarBeneficios(ctx context.Context, path string, payload any, result any) error {
	if s.beneficiosURL == "" || s.internalKey == "" {
		return errors.New("integración de beneficios no configurada")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.beneficiosURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Melt-Internal-Key", s.internalKey)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("beneficios respondió %d", resp.StatusCode)
	}
	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

func (s *servidorReservas) reservarBeneficio(ctx context.Context, canjeID, userID, cartID, treatmentID string, price float64) (canjeReservado, error) {
	var response struct {
		Canje canjeReservado `json:"canje"`
	}
	err := s.solicitarBeneficios(ctx, "/internal/canjes/"+url.PathEscape(canjeID)+"/reservar", map[string]any{"usuarioId": userID, "reservaId": cartID, "tratamientoId": treatmentID, "precio": price}, &response)
	return response.Canje, err
}

func (s *servidorReservas) actualizarBeneficio(ctx context.Context, canjeID, userID, cartID, action string) error {
	if canjeID == "" {
		return nil
	}
	return s.solicitarBeneficios(ctx, "/internal/canjes/"+url.PathEscape(canjeID)+"/"+action, map[string]string{"usuarioId": userID, "reservaId": cartID}, nil)
}

func (s *servidorReservas) liberarBeneficio(ctx context.Context, canjeID, userID, cartID string) {
	if err := s.actualizarBeneficio(ctx, canjeID, userID, cartID, "liberar"); err != nil {
		// La reserva del canje vence por diseño; registrar para poder diagnosticar
		// fallas transitorias sin impedir la liberación del horario.
		log.Printf("beneficios_release_failed canje=%s carrito=%s", canjeID, cartID)
	}
}
