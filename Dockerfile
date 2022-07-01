FROM golang as builder

WORKDIR /app/

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

COPY ./patches /patches

CMD ["go", "run", "main.go"]
