FROM golang:1.26.1-alpine AS build
WORKDIR /src
COPY . .
RUN go mod tidy && go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/reservas ./cmd/api

FROM alpine:3.22
RUN addgroup -S app && adduser -S app -G app && apk add --no-cache ca-certificates
USER app
COPY --from=build /out/reservas /usr/local/bin/reservas
EXPOSE 8082
ENTRYPOINT ["/usr/local/bin/reservas"]
