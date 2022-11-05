FROM golang as builder

WORKDIR /app/

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

RUN go build -o app .

COPY ./patches/. ./patches

CMD ["./app"]
