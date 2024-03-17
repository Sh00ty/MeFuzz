FROM golang:1.21 as builder

WORKDIR /app

RUN export GO111MODULE=on

COPY ./go.mod ./
COPY ./go.sum ./
RUN go mod download 
COPY . .

RUN CGO_ENABLED=0 go build -v -o ./orcestration ./cmd/

FROM alpine:latest

COPY --from=builder /app/orcestration .

EXPOSE 9990 9000

ENTRYPOINT ["./orcestration"]