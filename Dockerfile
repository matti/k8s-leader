FROM golang:1.19-alpine as builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./

RUN CGO_ENABLED=0 go build \
    -a -o k8s-leader main.go

FROM scratch
COPY --from=builder /build/k8s-leader /
ENTRYPOINT ["/k8s-leader"]
