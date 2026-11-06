FROM golang:1.26.1-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/pagos .

FROM alpine:3.22
RUN addgroup -S app && adduser -S app -G app
COPY --from=build /out/pagos /usr/local/bin/pagos
USER app
EXPOSE 8085
ENTRYPOINT ["/usr/local/bin/pagos"]
