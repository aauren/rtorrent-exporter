rtorrent-exporter [![GoDoc](http://godoc.org/github.com/aauren/rtorrent-exporter?status.svg)](http://godoc.org/github.com/aauren/rtorrent_exporter) ![Build Status](https://github.com/aauren/rtorrent-exporter/actions/workflows/ci.yml/badge.svg)
=================

This is an updated fork of [mdlayher/rtorrent_exporter](https://github.com/mdlayher/rtorrent_exporter). Much appreciation goes to them for
pioneering this package and putting effort into getting it off the ground.

Specific additions made by this fork:

* Support self-signed (non-official) certificates
* Allow setting timeouts on all HTTP / XMLRPC calls
* Allow disabling high-cardinality metrics (`-rtorrent.downloads.collect.details`)
* Improve performance for greater numbers of torrents (especially helpful if you have >100 torrents)

Command `rtorrent-exporter` provides a Prometheus exporter for rTorrent.

Package `rtorrentexporter` provides the Exporter type used in the `rtorrent_exporter` Prometheus exporter.

MIT Licensed.

Usage
-----

Available flags for `rtorrent-exporter` include:

```
% ./rtorrent-exporter --help
Usage of ./rtorrent-exporter:
  -rtorrent.addr string
        address of rTorrent XML-RPC server
  -rtorrent.downloads.collect.details
        [optional] collect rate and total bytes for each torrent (greatly increases metric cardinality) (defaults: true) (default true)
  -rtorrent.insecure
        [optional] allow using XML-RPC with a non-CA signed certificat (defaults: false)
  -rtorrent.password string
        [optional] password used for HTTP Basic authentication with rTorrent XML-RPC server
  -rtorrent.timeout duration
        [optional] duration of how long to wait before timing out rtorrent request (defaults: 10s) (default 10s)
  -rtorrent.username string
        [optional] username used for HTTP Basic authentication with rTorrent XML-RPC server
  -telemetry.addr string
        host:port for rTorrent exporter (default ":9135")
  -telemetry.path string
        URL path for surfacing collected metrics (default "/metrics")
  -telemetry.timeout duration
        [optional] duration of how long to wait to receive http headers on telemetry addr (defaults: 10s) (default 10s)
```

An example of using `rtorrent-exporter`:

```
$ ./rtorrent-exporter -rtorrent.addr http://127.0.0.1/RPC2
2016/03/09 17:39:40 starting rTorrent exporter on ":9135" for server "http://127.0.0.1/RPC2"
```

Docker
------

Docker Hub repo can be found here: [rtorrent-exporter](https://hub.docker.com/repository/docker/aauren/rtorrent-exporter/general)

```
docker run -ti --rm -p 9135:9135 --add-host=host.docker.internal:host-gateway "aauren/rtorrent-exporter:latest" -rtorrent.addr https://host.docker.internal/RPC2 -rtorrent.username "<http_basic_auth_user>" -rtorrent.password "<http_basic_auth_pass>" "-rtorrent.insecure" true
```

Docker Compose
--------------

See example here: [compose.yml](compose.yaml)

To start, run:

```
docker compose up -d
```

Sample
------

Here is a screenshot of a sample dashboard created using [`grafana`](https://github.com/grafana/grafana)
with metrics from exported from `rtorrent_exporter`.

![sample](https://cloud.githubusercontent.com/assets/1926905/13891308/bad263be-ed26-11e5-9601-9d770d95c538.png)
