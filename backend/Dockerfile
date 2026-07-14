FROM golang:1.23-alpine AS build
WORKDIR /app
RUN apk add --no-cache gcc musl-dev sqlite-dev
COPY backend/go.mod backend/go.sum* ./
COPY backend/backend.go ./
COPY backend/*.go ./
RUN go mod tidy && CGO_ENABLED=1 GOOS=linux go build -o scheduler .

FROM alpine:3.19
RUN apk add --no-cache sqlite tzdata
WORKDIR /app
COPY --from=build /app/scheduler /app/
COPY backend/frontend/dist /app/frontend/dist
EXPOSE 8080
ENTRYPOINT ["/app/scheduler"]
