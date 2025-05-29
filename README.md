rtorrent-exporter [![GoDoc](http://godoc.org/github.com/aauren/rtorrent-exporter?status.svg)](http://godoc.org/github.com/aauren/rtorrent-exporter) ![Build Status](https://github.com/aauren/rtorrent-exporter/actions/workflows/ci.yml/badge.svg)
=================

A metric exporter for rtorrent which scrapes the XML-RPC API that rtorrent exposes and then publishes it as metrics.

This is an example of the types of Grafana dashboards that can be made from these metrics:
![Grafana Example](/docs/rtorrent-screenshot.png)

This is an updated fork of [mdlayher/rtorrent_exporter](https://github.com/mdlayher/rtorrent_exporter). Much appreciation goes to them for
pioneering this package and putting effort into getting it off the ground.

Specific additions made by this fork:

* Support self-signed (non-official) certificates
* Allow setting timeouts on all HTTP / XMLRPC calls
* Allow disabling high-cardinality metrics (`--rtorrent.downloads.collect.details`)
* Reports torrents with errors via `rtorrent_downloads_error` gauge
* Reports tracker messages via `rtorrent_downloads_messages` (can be disabled by setting `--rtorrent.downloads.collect.messages` to `false`
* Adds tracker domains to the metric labels (can be disabled by setting `--rtorrent.trackers.enabled` to `false`)
* Improve performance for greater numbers of torrents (especially helpful if you have >100 torrents)
* Add ability to configure rtorrent-exporter via a configuration file (in addition to command-line parameters)

Command `rtorrent-exporter` provides a Prometheus exporter for rTorrent.

Package `rtorrentexporter` provides the Exporter type used in the `rtorrent_exporter` Prometheus exporter.

MIT Licensed.

Usage
-----

Available flags for `rtorrent-exporter` include:

```sh
% ./rtorrent-exporter --help
A Prometheus exporter for rTorrent that collects metrics such as download and upload rates, total downloaded and uploaded
                bytes, and tracker information.

Usage:
  rtorrent-exporter [flags]

Flags:
      --add_dir_header                                      If true, adds the file directory to the header of the log messages
      --alsologtostderr                                     log to standard error as well as files (no effect when -logtostderr=true)
      --config string                                       config file (default is $HOME/.rtorrent-exporter.yaml)
  -h, --help                                                help for rtorrent-exporter
      --log_backtrace_at traceLocation                      when logging hits line file:N, emit a stack trace (default :0)
      --log_dir string                                      If non-empty, write log files in this directory (no effect when -logtostderr=true)
      --log_file string                                     If non-empty, use this log file (no effect when -logtostderr=true)
      --log_file_max_size uint                              Defines the maximum size a log file can grow to (no effect when -logtostderr=true). Unit is megabytes. If the value is 0, the maximum file size is unlimited. (default 1800)
      --logtostderr                                         log to standard error instead of files (default true)
      --one_output                                          If true, only write logs to their native severity level (vs also writing to each lower severity level; no effect when -logtostderr=true)
      --rtorrent.addr string                                address of rTorrent XML-RPC server
      --rtorrent.downloads.collect.details                  [optional] collect rate and total bytes for each torrent (greatly increases metric cardinality) (default true)
      --rtorrent.downloads.collect.messages                 [optional] collect messages for each torrent (greatly increases metric cardinality) (default true)
      --rtorrent.insecure                                   [optional] allow using XML-RPC with a non-CA signed certificat (defaults: false)
      --rtorrent.password string                            [optional] password used for HTTP Basic authentication with rTorrent XML-RPC server
      --rtorrent.timeout duration                           [optional] duration of how long to wait before timing out rtorrent request (default 10s)
      --rtorrent.trackers.cache.max-age duration            [optional] maximum age of a cached tracker before it is considered stale (default 1h0m0s)
      --rtorrent.trackers.cache.max-parallel-requests int   [optional] maximum number of parallel requests that will be made to rtorrent at a time for fetching tracker information (default 5)
      --rtorrent.trackers.cache.min-age duration            [optional] minimum age of a cached tracker before it is considered for refresh (default 10m0s)
      --rtorrent.trackers.enabled                           [optional] enable tracking of tracker information (increases metric cardinality and load on rTorrent server) (default true)
      --rtorrent.username string                            [optional] username used for HTTP Basic authentication with rTorrent XML-RPC server
      --skip_headers                                        If true, avoid header prefixes in the log messages
      --skip_log_headers                                    If true, avoid headers when opening log files (no effect when -logtostderr=true)
      --stderrthreshold severity                            logs at or above this threshold go to stderr when writing to files and stderr (no effect when -logtostderr=true or -alsologtostderr=true) (default 2)
      --telemetry.addr string                               host:port for rTorrent exporter (default ":9135")
      --telemetry.path string                               URL path for surfacing collected metrics (default "/metrics")
      --telemetry.timeout duration                          [optional] duration of how long to wait to receive http headers on telemetry addr (default 10s)
  -v, --v Level                                             number for the log level verbosity
      --viper                                               use Viper for configuration (default true)
      --vmodule moduleSpec                                  comma-separated list of pattern=N settings for file-filtered logging
      --write-config                                        write the current configuration to the config file and exit
```

An example of using `rtorrent-exporter` with a specific endpoint:

```sh
$ ./rtorrent-exporter --rtorrent.addr http://127.0.0.1/RPC2
2016/03/09 17:39:40 starting rTorrent exporter on ":9135" for server "http://127.0.0.1/RPC2"
```

Example where tracker fetching is disabled

```sh
./rtorrent-exporter --rtorrent.addr http://127.0.0.1/RPC2 --rtorrent.trackers.enabled=false
```

More complete example where username / password are specified along with insecure for using an unsigend certificate, along with specifying
`-v 1` to enable more verbose logging

```sh
./rtorrent-exporter --rtorrent.addr "https://127.0.0.1/RPC2" --rtorrent.username "<your-user-here>" --rtorrent.password "<pass-here>" \
--rtorrent.insecure=true --rtorrent.trackers.cache.max-parallel-requests 10 -v 1
```

Config File
-----------

rtorrent-exporter can also be configured via a config file instead of parameters. In fact, this is the **only** way to configure certain
options that would be too verbose to specify via command parameters (such as tracker name substitutions).

The easiest way to start off getting a configuration file, is to run rtorrent-exporter the same way you would with command parameters, but
then add: `--write-config` to the end of the command.

This will produce a config file at: `${HOME}/.rtorrent-exporter.yaml` that should have a basic outline of all of your options already filled
in. From this point forward, you should be able to run rtorrent-exporter without any command parameters and have it operate as it did
before.

Here is an example configuration file:

```yaml
rtorrent:
    addr: https://<your-server>/RPC2
    downloads:
        collect:
            details: true
            messages: true
    insecure: true
    password: <password>
    timeout: 10s
    trackers:
        cache:
            max-age: 1h0m0s
            max-parallel-requests: 5
            min-age: 10m0s
        enabled: true
        tracker-name-substitutions:
          - convert-to: Ubuntu
            matchers:
              - ^.*\.ubuntu\.com$
          - convert-to: Debian
            matchers:
              - ^bttracker\.debian\.org$
              - ^.*\.debian\.org$
    username: <username>
telemetry:
    addr: :9135
    path: /metrics
    timeout: 10s
useviper: true
```

Tracker Name Substitution
-------------------------

As you may have noticed above, rtorrent-exporter contains the ability to do advanced tracker domain name substitution. This helps in case
the domain for your tracker is obfuscated or in the case where you have a torrent download that has several different tracker URLs and you
want to be able to aggregate metrics across them.

Each stanza has a `convert-to` name which will be the name of the Tracker that you'll see in the metrics in the `tracker` metric label if
one of the matchers has matched this entry.

Each name substitution can contain one or more `matchers` which are regular expressions that will be matched against the tracker's domain
name. If there are any problems compiling your regular expressions, you'll be notified via an error at startup.

A Note About Enabling Some Options
----------------------------------

Not all options can be enabled without certain trade-offs. Below I attempt to describe what some of the trade-offs are for various options.

* `--rtorrent.downloads.collect.details` - Adds metrics that will tell the user how much bytes the torrent has transferred which is
  one of the key metrics produced by this exporter. However, it will also add the torrent hash and the torrent name as a label to each of
  these metrics which will greatly increase the cardinality of metrics produced. If you have a lot of metrics, this may end up hurting your
  Prometheus performance as each of these is a unique value.
* `--rtorrent.trackers.enabled` - Adds an additional label to all of the detailed metrics that will tell you the domain of the tracker that
  the individual torrent uploaded against. This is really helpful if you want to see coarse statistics by tracker. However, the only way to
  get this information from rtorrent is to request it for each individual torrent.
  * Because of this rtorrent-exporter has a cache that allows rtorrent-exporter to keep from having to continually request this information
    and reduces load on the rtorrent instance. However, it still has to populate this information when it initially starts, so this results
    in the following:
    * Initial load of the tracker label may be slow to populate depending on how many torrents you have
    * Each restart of rtorrent-exporter will cause a cascade of requests to the rtorrent server to initially populate the cache
  * You can tune how many requests are made at once by setting: `--rtorrent.trackers.cache.max-parallel-requests`
  * You can affect when trackers are considered stale within the cache by adjusting: `--rtorrent.trackers.cache.min-age` and
    `--rtorrent.trackers.cache.max-age`

By default all of the above options are enabled, unless you disable them specifically.

Troubleshooting
---------------

If something isn't working for you correctly, you can increase the verbosity of the messages output by `rtorrent-exporter` by specifying
`-v 1` or `-v 2` as input arguments to the application. Increasing the number passed to the `-v` parameter will output more verbose logs.

Docker
------

Docker Hub repo can be found here: [rtorrent-exporter](https://hub.docker.com/repository/docker/aauren/rtorrent-exporter/general)

```sh
docker run -ti --rm -p 9135:9135 --add-host=host.docker.internal:host-gateway "aauren/rtorrent-exporter:latest" --rtorrent.addr https://host.docker.internal/RPC2 --rtorrent.username "<http_basic_auth_user>" --rtorrent.password "<http_basic_auth_pass>" --rtorrent.insecure true
```

Docker Compose
--------------

See example here: [compose.yml](compose.yaml)

To start, run:

```sh
docker compose up -d
```

Grafana Dashbaord
------

There is an example Grafana dashboard that user's can use contained within this repository at [grafana-dashboard.json](/resources/grafana-dashboard.json)

You can also find an example of the dashboard on Grafana's public dashboard service [rtorrent-exporter](https://grafana.com/grafana/dashboards/22581)
