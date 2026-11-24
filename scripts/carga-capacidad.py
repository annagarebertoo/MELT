#!/usr/bin/env python3
"""Prueba de capacidad escalonada para MELT (D13).

Envía solicitudes GET con concurrencia creciente y registra, por escalón,
throughput, latencias p50/p95/p99 y errores. Solo usa la biblioteca estándar
para poder correr dentro de un contenedor efímero conectado a la red interna
del entorno aislado `melt-s1` (que no publica puertos hacia el host):

    docker run --rm --network melt-s1_s1 -v "$PWD/scripts:/s:ro" -v "$PWD/output:/out" \
      python:3.13-alpine python /s/carga-capacidad.py \
      --url "http://gateway/api/v1/catalogo/tratamientos?page=1&limit=10" \
      --escalones 1,2,4,8,16,32,64 --segundos 15 --salida /out/capacidad/catalogo-listado.json

Nunca apuntar este script a Render ni a bases compartidas: solo genera lecturas,
pero la carga sostenida puede consumir cuota de servicios gratuitos.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import math
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

HOSTS_PERMITIDOS = {"gateway", "localhost", "127.0.0.1"}


def percentil(valores: list[float], p: float) -> float | None:
    if not valores:
        return None
    ordenados = sorted(valores)
    k = max(0, min(len(ordenados) - 1, math.ceil(p / 100 * len(ordenados)) - 1))
    return round(ordenados[k], 2)


def escalon(url: str, concurrencia: int, segundos: float, timeout: float) -> dict:
    fin = time.monotonic() + segundos
    latencias: list[float] = []
    estados: dict[str, int] = {}
    lock = threading.Lock()

    def trabajador(n: int) -> None:
        secuencia = 0
        while time.monotonic() < fin:
            secuencia += 1
            req = urllib.request.Request(url, headers={"X-Request-ID": f"cap-c{concurrencia}-w{n}-{secuencia}"})
            inicio = time.perf_counter()
            try:
                with urllib.request.urlopen(req, timeout=timeout) as resp:
                    resp.read()
                    estado = str(resp.status)
            except urllib.error.HTTPError as error:
                estado = str(error.code)
            except Exception as error:  # timeout, conexión rechazada, etc.
                estado = type(error).__name__
            duracion = (time.perf_counter() - inicio) * 1000
            with lock:
                estados[estado] = estados.get(estado, 0) + 1
                if estado == "200":
                    latencias.append(duracion)

    hilos = [threading.Thread(target=trabajador, args=(i,)) for i in range(concurrencia)]
    inicio = time.monotonic()
    for h in hilos:
        h.start()
    for h in hilos:
        h.join()
    transcurrido = time.monotonic() - inicio
    total = sum(estados.values())
    exitosas = estados.get("200", 0)
    return {
        "concurrencia": concurrencia,
        "segundos": round(transcurrido, 3),
        "total": total,
        "exitosas": exitosas,
        "fallidas": total - exitosas,
        "tasa_error": round((total - exitosas) / total, 4) if total else None,
        "estados": estados,
        "throughput_rps": round(exitosas / transcurrido, 2) if transcurrido else None,
        "latencia_ms": {
            "p50": percentil(latencias, 50),
            "p95": percentil(latencias, 95),
            "p99": percentil(latencias, 99),
            "max": round(max(latencias), 2) if latencias else None,
        },
    }


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--url", required=True)
    parser.add_argument("--escalones", default="1,2,4,8,16,32,64", help="concurrencias separadas por coma")
    parser.add_argument("--segundos", type=float, default=15.0, help="duración de cada escalón (5-120)")
    parser.add_argument("--timeout", type=float, default=5.0)
    parser.add_argument("--calentamiento", type=float, default=3.0, help="segundos de tráfico previo no medido")
    parser.add_argument("--salida", required=True, help="archivo JSON de resultados (no se sobrescribe)")
    args = parser.parse_args()

    host = urllib.parse.urlparse(args.url).hostname or ""
    if host not in HOSTS_PERMITIDOS:
        parser.error(f"host {host!r} no permitido: usar solo el Gateway del entorno local aislado")
    if not 5 <= args.segundos <= 120:
        parser.error("--segundos debe estar entre 5 y 120")
    escalones = [int(x) for x in args.escalones.split(",") if x.strip()]
    if not escalones or any(c < 1 or c > 128 for c in escalones):
        parser.error("cada escalón debe estar entre 1 y 128")
    salida = Path(args.salida)
    if salida.exists():
        parser.error(f"{salida} ya existe; elegí otro nombre")
    salida.parent.mkdir(parents=True, exist_ok=True)

    if args.calentamiento > 0:
        escalon(args.url, 2, args.calentamiento, args.timeout)

    resultados = []
    for c in escalones:
        r = escalon(args.url, c, args.segundos, args.timeout)
        r["inicio_utc"] = dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds")
        resultados.append(r)
        print(f"c={c:>3} rps={r['throughput_rps']:>8} p50={r['latencia_ms']['p50']} "
              f"p95={r['latencia_ms']['p95']} p99={r['latencia_ms']['p99']} errores={r['fallidas']}", flush=True)

    mejor = max(resultados, key=lambda r: r["throughput_rps"] or 0)
    informe = {
        "schema": "melt-capacidad/v1",
        "url": args.url,
        "segundos_por_escalon": args.segundos,
        "timeout_s": args.timeout,
        "escalones": resultados,
        "throughput_maximo_rps": mejor["throughput_rps"],
        "concurrencia_del_maximo": mejor["concurrencia"],
        "nota": "El generador corre en un contenedor Python con hilos; por encima de ~64 hilos el propio generador puede limitar.",
    }
    salida.write_text(json.dumps(informe, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"informe: {salida}")


if __name__ == "__main__":
    main()
