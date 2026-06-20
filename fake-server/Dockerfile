FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/fake-server ./cmd/fake-server

FROM gcr.io/distroless/base-debian12
COPY --from=build /out/fake-server /fake-server
EXPOSE 8090 8091 8092 8093
ENTRYPOINT ["/fake-server"]
