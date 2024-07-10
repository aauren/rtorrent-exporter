FROM golang:1.22.5-alpine3.19

EXPOSE 9135

WORKDIR /go/src/app
COPY . .

RUN go install -v ./...

ENTRYPOINT ["rtorrent_exporter"]
