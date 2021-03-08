FROM golang:1.16-alpine3.13 AS builder
WORKDIR /src
COPY . /src
RUN CGO_ENABLED=0 GOOS=linux go build -o starlink-exporter

FROM alpine:3.13  
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /src/starlink-exporter .
CMD ["./starlink-exporter"]  
