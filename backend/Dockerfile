FROM golang:1.25-bookworm AS build
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends build-essential pkg-config && rm -rf /var/lib/apt/lists/*
COPY backend/go.mod backend/go.sum* ./
COPY backend/*.go ./
RUN go mod tidy && CGO_ENABLED=1 GOOS=linux go build -o scheduler .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends sqlite3 ca-certificates tzdata && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /app/scheduler /app/
COPY backend/frontend/dist/ /app/frontend/dist/
EXPOSE 9006
ENTRYPOINT ["/app/scheduler"]
