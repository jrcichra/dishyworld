package main

import (
	"container/ring"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	pb "github.com/starlink-community/starlink-grpc-go/pkg/spacex.com/api/device"
)

var (
	wifiDeviceInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "wifi_device_info",
	},
		[]string{"id", "hardware_version", "software_version", "country_code", "sku"},
	)
	wifiPingDropRate = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "wifi_ping_drop_rate",
	})
	wifiPingLatencyMs = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "wifi_ping_latency_ms",
	})

	wifiPingResultDropRate = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "wifi_ping_report_drop_rate",
	},
		[]string{"service", "location", "address"},
	)

	wifiPingResultLatencyMs = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "wifi_ping_report_latency_ms",
	},
		[]string{"service", "location", "address"},
	)

	dishDeviceInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dish_device_info",
	},
		[]string{"id", "hardware_version", "software_version", "country_code"},
	)
	dishPopPingDropRate = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dish_pop_ping_drop_rate",
	})
	dishPopPingLatencyMs = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dish_pop_ping_latency_ms",
	})
	dishSnr = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dish_snr",
	})
	dishUplinkThroughputBps = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dish_uplink_throughput_bps",
	})
	dishDownlinkThroughputBps = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dish_downlink_throughput_bps",
	})
	dishCurrentlyObstructed = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dish_currently_obstructed",
	})
	dishFractionObstructed = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dish_fraction_obstructed",
	})
	dishLast24hObstructedS = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dish_last_24h_obstructed_s",
	})
	dishValidS = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dish_valid_s",
	})
	dishWedgeFractionObstructed = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dish_wedge_fraction_obstructed",
	},
		[]string{"degrees"},
	)
	dishWedgeAbsFractionObstructed = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dish_wedge_abs_fraction_obstructed",
	},
		[]string{"degrees"},
	)

	dishAddress    = "192.168.100.1:9200"
	wifiAddress    = "192.168.1.1:9000"
	metricsAddress = "0.0.0.0:2112"

	statusInterval  = time.Duration(4) * time.Minute
	pingInterval    = time.Duration(1) * time.Minute
	historyInterval = time.Duration(20) * time.Second
	retryInterval   = time.Duration(2) * time.Second
	resetInterval   = time.Duration(1) * time.Hour
)

func init() {
	flag.StringVar(&dishAddress, "dish_addr", dishAddress, "Dishy's address")
	flag.StringVar(&wifiAddress, "wifi_addr", wifiAddress, "Wifi address")
	flag.StringVar(&metricsAddress, "metrics_addr", metricsAddress, "/metrics address")
	flag.DurationVar(&statusInterval, "status_interval", statusInterval, "Status metrics polling interval.")
	flag.DurationVar(&pingInterval, "ping_interval", pingInterval, "Ping metrics polling interval.")
	flag.DurationVar(&historyInterval, "history_duration", historyInterval, "Polls history this often, then replays it. This means the current metrics from history will be delayed by this amount because of the history replay, but allows us to poll less frequently. Dishy DVR!")
	flag.Parse()
}

func resetTimer() {
	go func() {
		time.Sleep(resetInterval)
		panic("Intentional program death, to keep dishy alive. The restart=unless-stopped should restart us")
	}()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func bool2Float64(b bool) float64 {
	if b {
		return float64(1)
	}
	return float64(0)
}

func recordHistoryMetrics() {
	go func() {
		for {
			conn, err := grpc.Dial(dishAddress, grpc.WithInsecure(), grpc.WithBlock())
			defer conn.Close()
			if err != nil {
				fmt.Println("[dish] could not connect:", err)
				time.Sleep(retryInterval)
				return
			}
			c := pb.NewDeviceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			in := new(pb.Request)
			in.Request = &pb.Request_GetHistory{}

			resp, err := c.Handle(ctx, in)
			if err != nil {
				fmt.Println("[history] could not get status:", err)
				time.Sleep(retryInterval)
				continue
			}
			hist := resp.GetDishGetHistory()
			current := int(hist.Current)

			allSeries := map[prometheus.Gauge][]float32{
				dishPopPingLatencyMs:      hist.PopPingLatencyMs,
				dishPopPingDropRate:       hist.PopPingDropRate,
				dishDownlinkThroughputBps: hist.DownlinkThroughputBps,
				dishUplinkThroughputBps:   hist.UplinkThroughputBps,
				dishSnr:                   hist.Snr,
			}
			wg := &sync.WaitGroup{}
			more_sleep := 0
			for metric, series := range allSeries {
				wg.Add(1)
				num_samples := int(historyInterval.Seconds())
				orderedHistory := reorderSeries(series, current, num_samples)
				more_sleep = num_samples - len(orderedHistory)
				go func(wg *sync.WaitGroup, metric prometheus.Gauge, orderedHistory []float32) {
					defer wg.Done()
					for i := 0; i < len(orderedHistory); i++ {
						metric.Set(float64(orderedHistory[i]))
						time.Sleep(1 * time.Second)
					}
				}(wg, metric, orderedHistory)
			}
			time.Sleep(time.Duration(more_sleep) * time.Second)
			wg.Wait()
		}

	}()

}

func reorderSeries(series []float32, current int, parse_samples int) []float32 {
	if parse_samples < 0 || parse_samples > len(series) {
		parse_samples = len(series)
	}
	if current < parse_samples {
		parse_samples = current + 1
	}
	r := ring.New(len(series))
	for i := 0; i < len(series); i++ {
		r.Value = series[i]
		r = r.Next()
	}
	r = r.Move(current + 1)
	r = r.Move(parse_samples * -1)
	samples := []float32{}
	for i := 0; i < parse_samples; i++ {
		samples = append(samples, r.Value.(float32))
		r = r.Next()
	}
	return samples

}

func recordPingMetrics() {
	go func() {
		for {
			conn, err := grpc.Dial(wifiAddress, grpc.WithInsecure(), grpc.WithBlock())
			if err != nil {
				fmt.Println("[ping] could not connect:", err)
				time.Sleep(retryInterval)
				continue
			}
			c := pb.NewDeviceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			in := new(pb.Request)
			in.Request = &pb.Request_GetPing{}

			resp, err := c.Handle(ctx, in)
			if err != nil {
				fmt.Println("[wifi] could not get status:", err)
				time.Sleep(retryInterval)
				continue
			}
			ping := resp.GetGetPing()
			for _, v := range ping.GetResults() {
				l := prometheus.Labels{
					"service":  v.Target.Service,
					"location": v.Target.Location,
					"address":  v.Target.Address,
				}
				wifiPingResultDropRate.With(l).Set(float64(v.DropRate))
				wifiPingResultLatencyMs.With(l).Set(float64(v.LatencyMs))

			}
			conn.Close()
			time.Sleep(pingInterval)
		}

	}()
}

func recordWifiMetrics() {
	go func() {
		for {
			conn, err := grpc.Dial(wifiAddress, grpc.WithInsecure(), grpc.WithBlock())
			if err != nil {
				fmt.Println("[wifi] could not connect:", err)
				time.Sleep(retryInterval)
				continue
			}
			c := pb.NewDeviceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			in := new(pb.Request)
			in.Request = &pb.Request_GetStatus{}

			resp, err := c.Handle(ctx, in)
			if err != nil {
				fmt.Println("[wifi] could not get status:", err)
				time.Sleep(retryInterval)
				continue
			}
			status := resp.GetWifiGetStatus()
			info := status.GetDeviceInfo()

			l := prometheus.Labels{
				"id":               info.GetId(),
				"hardware_version": info.GetHardwareVersion(),
				"software_version": info.GetSoftwareVersion(),
				"country_code":     info.GetCountryCode(),
				"sku":              status.GetSku(),
			}
			wifiDeviceInfo.With(l).Set(1)

			wifiPingDropRate.Set(float64(status.PingDropRate))
			wifiPingLatencyMs.Set(float64(status.PingLatencyMs))

			conn.Close()
			time.Sleep(statusInterval)
		}
	}()
}
func recordDishMetrics() {
	go func() {

		for {
			conn, err := grpc.Dial(dishAddress, grpc.WithInsecure(), grpc.WithBlock())
			if err != nil {
				fmt.Println("[dish] could not connect:", err)
				time.Sleep(retryInterval)
				continue
			}
			c := pb.NewDeviceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			in := new(pb.Request)
			in.Request = &pb.Request_GetStatus{}

			resp, err := c.Handle(ctx, in)
			if err != nil {
				fmt.Println("[dish] could not get status:", err)
				time.Sleep(retryInterval)
				continue
			}

			status := resp.GetDishGetStatus()
			info := status.GetDeviceInfo()
			obs := status.GetObstructionStats()

			l := prometheus.Labels{
				"id":               info.GetId(),
				"hardware_version": info.GetHardwareVersion(),
				"software_version": info.GetSoftwareVersion(),
				"country_code":     info.GetCountryCode(),
			}
			dishDeviceInfo.With(l).Set(1)

			dishCurrentlyObstructed.Set(bool2Float64(obs.CurrentlyObstructed))
			dishFractionObstructed.Set(float64(obs.FractionObstructed))
			dishLast24hObstructedS.Set(float64(obs.Last_24HObstructedS))
			dishValidS.Set(float64(obs.ValidS))
			degrees := 0
			for _, v := range obs.WedgeFractionObstructed {
				l := prometheus.Labels{
					"degrees": strconv.Itoa(degrees),
				}
				degrees += 30
				dishWedgeFractionObstructed.With(l).Set(float64(v))

			}
			degrees = 0
			for _, v := range obs.WedgeAbsFractionObstructed {
				l := prometheus.Labels{
					"degrees": strconv.Itoa(degrees),
				}
				degrees += 30
				dishWedgeAbsFractionObstructed.With(l).Set(float64(v))
			}

			conn.Close()
			time.Sleep(statusInterval)
		}
	}()
}
func checkWifi() (string, bool, error) {
	conn, err := grpc.Dial(wifiAddress, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithTimeout(1*time.Second))
	if err != nil {
		fmt.Println("[wifi] could not connect:", err)
		return "", false, err
	}
	c := pb.NewDeviceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	in := new(pb.Request)
	in.Request = &pb.Request_GetStatus{}

	resp, err := c.Handle(ctx, in)
	if err != nil {
		fmt.Println("[wifi] could not get status:", err)
		return "", false, err
	}
	status := resp.GetWifiGetStatus()
	info := status.GetDeviceInfo()
	return info.GetId(), true, nil
}

func checkDish() (string, bool, error) {
	conn, err := grpc.Dial(dishAddress, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithTimeout(1*time.Second))
	if err != nil {
		fmt.Println("[dish] could not connect:", err)
		return "", false, err
	}
	c := pb.NewDeviceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	in := new(pb.Request)
	in.Request = &pb.Request_GetStatus{}

	resp, err := c.Handle(ctx, in)
	if err != nil {
		fmt.Println("[dish] could not get status:", err)
		return "", false, err
	}
	status := resp.GetDishGetStatus()
	info := status.GetDeviceInfo()
	return info.GetId(), true, nil
}

func main() {

	_, wifiOk, err := checkWifi()
	if err != nil {
		fmt.Println("[wifi] disabling wifi checks")
	}
	if err != nil {
		fmt.Println("[dish] cannot run without dish, exiting... are you running from the starlink network?")
		os.Exit(1)
	}
	resetTimer()
	recordDishMetrics()
	if wifiOk {
		recordWifiMetrics()
	}
	recordPingMetrics()
	recordHistoryMetrics()

	fmt.Printf("[starlink-exporter] started metrics on http://%s/metrics\n", metricsAddress)
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(metricsAddress, nil)
}
