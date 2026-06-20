FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/service-workflow ./cmd/service-workflow

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build /out/service-workflow /app/service-workflow
COPY config /app/config
EXPOSE 8080
ENTRYPOINT ["/app/service-workflow"]
