FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/go-service-platform ./cmd/go-service-platform

FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /app
COPY --from=build /out/go-service-platform /app/go-service-platform
COPY config /app/config
ENV ADMIN_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/app/go-service-platform"]
