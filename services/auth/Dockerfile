FROM golang:1.26.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/auth ./cmd/api

FROM alpine:3.22
RUN addgroup -S app && adduser -S app -G app && apk add --no-cache ca-certificates
USER app
COPY --from=build /out/auth /usr/local/bin/auth
EXPOSE 8084
ENTRYPOINT ["/usr/local/bin/auth"]
