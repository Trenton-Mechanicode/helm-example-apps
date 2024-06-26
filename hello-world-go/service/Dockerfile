FROM golang:alpine AS builder

WORKDIR /app/builder

COPY app .

RUN go mod init azinterview-go
RUN go mod tidy
RUN go get azinterview-go
RUN go build -o main ./...

FROM alpine AS final

WORKDIR /app

RUN adduser -D -u 1337 -g 1337 mechanicode

RUN apk add --no-cache docker kubectl
COPY --from=builder /app/builder/main /app/main

COPY app/ /app/

EXPOSE 8080

RUN chown -R mechanicode: /app

# USER mechanicode

CMD ["/app/main"]
