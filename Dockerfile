FROM golang:1.11-alpine as builder
WORKDIR /go/src/github.com/cofyc/k8s-rebalancer
ADD . .
RUN CGO_ENABLED=0 go build -o k8s-rebalancer github.com/cofyc/k8s-rebalancer/cmd/k8s-rebalancer

FROM alpine:latest

COPY --from=builder /go/src/github.com/cofyc/k8s-rebalancer/k8s-rebalancer /usr/local/bin/k8s-rebalancer
ENTRYPOINT ["/usr/local/bin/k8s-rebalancer"]
