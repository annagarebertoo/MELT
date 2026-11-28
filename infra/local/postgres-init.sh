#!/bin/sh
set -eu

psql --username "$POSTGRES_USER" --dbname postgres --set ON_ERROR_STOP=1 <<'SQL'
CREATE DATABASE reservas;
CREATE DATABASE beneficios;
SQL

psql --username "$POSTGRES_USER" --dbname reservas --set ON_ERROR_STOP=1 --file=/melt-schemas/reservas.sql
psql --username "$POSTGRES_USER" --dbname beneficios --set ON_ERROR_STOP=1 --file=/melt-schemas/beneficios.sql
