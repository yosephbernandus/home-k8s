FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY main.go .
RUN go mod init deepseek-interface && go build -o main .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/main .
EXPOSE 8080
CMD ["./main"]
