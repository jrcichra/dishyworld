# jrcichra fork
+ I've simplified this exporter down to purely expose Dishy's gRPC data. I didn't want to run a speed test or have a prometheus cache built into the exporter
+ Scraping data with browsers visiting 192.168.100.1 breaks the Starlink router. Most of the data I need seems to be exposed through gRPC
+ I'll leave Grafana charts up to the user. Please see the great work done by the original author: https://github.com/starlink-community/dishyworld for Grafana charts and other examples
# Welcome

This project aims to make it super easy for Starlink users to discover how their Starlink installation is performing. It is intended for Starlink customers that would like to customize their own dashboard. You will need to be comfortable with prometheus metrics and grafana in order to customize the dashboard.

![dishy dashboard](../media/dishy-dashboard.png?raw=true)

Features:
  * ~~Hourly speed tests using Speedtest.net~~
  * Easily customizable with Grafana
  * Prometheus available for ad-hoc queries
  * Highest latency remote servers from the wifi router
  * Service wide aggregation of latency
  * Failsafes for networks with more than WAN
  * Minimal polling of your Starlink infrastructure
  * Written in golang, so very portable across Windows, Mac, and Linux
  * Utilizes Starlink native GRPC APIs

If you would prefer to just have a dashboard, without all the customization features, the official dashboard can be found in the Starlink App or on the dish itself at http://192.168.100.1/support/statistics


# Running just the exporter, BYO Prometheus

If you would like to monitor with your own prometheus instance, you can run just the exporter by following the development instructions, then running the agent:

```
$ go build -o starlink-exporter cmd/starlink-exporter/main.go
$ ./starlink-exporter -h
Usage of ./starlink-exporter:
  -dish_addr string
    	Dishy's address (default "192.168.100.1:9200")
  -history_duration duration
    	Polls history this often, then replays it. This means the current metrics from history will be delayed by this amount because of the history replay, but allows us to poll less frequently. Dishy DVR! (default 2m0s)
  -metrics_addr string
    	/metrics address (default "127.0.0.1:2112")
  -ping_interval duration
    	Ping metrics polling interval. (default 1m0s)
  -status_interval duration
    	Status metrics polling interval. (default 4m0s)
  -wifi_addr string
    	Wifi address (default "192.168.1.1:9000")
...
```

By default `http://localhost:2112/metrics` will be available for your promethues to poll. 