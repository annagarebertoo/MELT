package main

import (
	"testing"

	"github.com/rabbitmq/amqp091-go"
)

func TestValidacionDeRecompensas(t *testing.T) {
	tests := []struct {
		name string
		item Recompensa
		valid bool
	}{
		{name: "descuento válido", item: Recompensa{ID: "diez", Nombre: "10%", Tipo: "descuento_porcentaje", CostoPuntos: 20, Porcentaje: 10}, valid: true},
		{name: "tratamiento válido", item: Recompensa{ID: "facial", Nombre: "Facial sin cargo", Tipo: "tratamiento_gratis", CostoPuntos: 50, TratamientoID: "facial-1"}, valid: true},
		{name: "costo cero", item: Recompensa{ID: "diez", Nombre: "10%", Tipo: "descuento_porcentaje", Porcentaje: 10}},
		{name: "porcentaje fuera de rango", item: Recompensa{ID: "diez", Nombre: "10%", Tipo: "descuento_porcentaje", CostoPuntos: 20, Porcentaje: 101}},
		{name: "tratamiento sin id", item: Recompensa{ID: "facial", Nombre: "Facial", Tipo: "tratamiento_gratis", CostoPuntos: 50}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := recompensaValida(test.item); got != test.valid {
				t.Fatalf("recompensaValida()=%v, esperado %v", got, test.valid)
			}
		})
	}
}

func TestConteoDeReintentosRabbitMQ(t *testing.T) {
	for _, test := range []struct {
		header any
		want int
	}{{int32(2), 2}, {int64(3), 3}, {"4", 4}, {"inválido", 0}, {nil, 0}} {
		table := amqp091.Table{}
		if test.header != nil {
			table["x-melt-intentos"] = test.header
		}
		if got := retryCount(table); got != test.want {
			t.Fatalf("retryCount(%v)=%d, esperado %d", test.header, got, test.want)
		}
	}
}
