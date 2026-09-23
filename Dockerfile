# Build frontend
FROM node:20-slim AS frontend
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Build backend
FROM golang:1.22-alpine AS backend
WORKDIR /app
COPY go.* ./
RUN go mod download
COPY . .
COPY --from=frontend /app/static ./static
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o mimo-web-proxy .

# Runtime
FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=backend /app/mimo-web-proxy .
EXPOSE 8080
CMD ["./mimo-web-proxy"]
