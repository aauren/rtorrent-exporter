ARG BUILDTIME_BASE=golang:1.22.5
ARG RUNTIME_BASE=gcr.io/distroless/static:latest
FROM ${BUILDTIME_BASE} AS builder

WORKDIR /go/src/app
ENV CGO_ENABLED=0
COPY . /go/src/app
EXPOSE 9135

RUN go build -ldflags '-s -w' -o /go/bin/rtorrent-exporter cmd/rtorrent_exporter/main.go

FROM ${RUNTIME_BASE}

COPY --from=builder /go/bin/rtorrent-exporter /
CMD ["/rtorrent-exporter"]
