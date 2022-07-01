FROM golang:1.18.3-bullseye as builder

WORKDIR /build

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN go build -o ./app .

FROM debian:bookworm

COPY --from=builder /build/app /app
COPY ./patches /patches

ENTRYPOINT ["/app"]