ARG BUILDTIME_BASE=golang:1.26.1
ARG RUNTIME_BASE=gcr.io/distroless/static:latest
FROM ${BUILDTIME_BASE} AS builder
ENV BUILD_IN_DOCKER=false

WORKDIR /build
ENV CGO_ENABLED=0
COPY . /build
EXPOSE 9135

RUN make rtorrent-exporter

FROM ${RUNTIME_BASE}

COPY --from=builder /build/rtorrent-exporter /
CMD ["/rtorrent-exporter"]
