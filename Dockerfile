FROM golang:1.26-alpine3.24 AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a static binary that runs on the bare alpine stage.
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/api

FROM alpine:3.24 AS prod
WORKDIR /app
COPY --from=build /app/main /app/main

# Documentation only; the published mapping is set in docker-compose.yml.
ARG PORT=9090
EXPOSE ${PORT}
CMD ["./main"]
