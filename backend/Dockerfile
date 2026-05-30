FROM golang:1.25-alpine AS build
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum* ./
RUN go mod download
COPY backend/ ./
RUN go build -o /out/server ./cmd/server

FROM alpine:3.21
WORKDIR /app/backend
COPY --from=build /out/server /usr/local/bin/afkh-server
COPY data /app/data
EXPOSE 8080
CMD ["afkh-server"]
