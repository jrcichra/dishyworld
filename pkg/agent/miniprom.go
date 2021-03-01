package agent

import (
	"context"
	"fmt"
	"os"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/storage/remote"
	"github.com/prometheus/prometheus/tsdb"

	_ "embed"
	"time"
)

//go:embed template/config.yaml
var configTemplate string

//go:embed template/default.secret
var defaultURL string

type MiniProm struct {
	logger  log.Logger
	conf    *config.Config
	tsdbDir string
	addr    string
}

func NewMiniProm(tsdbDir string, addr string, dishId string) (*MiniProm, error) {
	ll := &promlog.AllowedLevel{}
	ll.Set("warn")
	logger := promlog.New(&promlog.Config{Level: ll})

	s := fmt.Sprintf(configTemplate, dishId)
	fmt.Println(s)
	cfg, err := config.Load(s)
	if err != nil {
		return nil, err
	}
	return &MiniProm{
		tsdbDir: tsdbDir,
		logger:  logger,
		conf:    cfg,
		addr:    addr,
	}, nil
}
func (m *MiniProm) Start() {
	db, err := tsdb.Open(
		m.tsdbDir,
		log.With(m.logger, "component", "tsdb"),
		prometheus.DefaultRegisterer,
		nil,
	)
	if err != nil {
		fmt.Println("db failed", err)
		os.Exit(1)
	}
	var (
		localStorage           = db
		remoteStorage          = remote.NewStorage(log.With(m.logger, "component", "remote"), prometheus.DefaultRegisterer, localStorage.StartTime, m.tsdbDir, time.Duration(1*time.Minute), nil)
		fanoutStorage          = storage.NewFanout(m.logger, localStorage, remoteStorage)
		scrapeManager          = scrape.NewManager(log.With(m.logger, "component", "scrape manager"), fanoutStorage)
		ctxScrape, _           = context.WithCancel(context.Background())
		discoveryManagerScrape = discovery.NewManager(ctxScrape, log.With(m.logger, "component", "discovery manager scrape"), discovery.Name("scrape"))
	)
	scrapeManager.ApplyConfig(m.conf)
	remoteStorage.ApplyConfig(m.conf)
	c := make(map[string]discovery.Configs)
	for _, v := range m.conf.ScrapeConfigs {
		c[v.JobName] = v.ServiceDiscoveryConfigs
	}
	discoveryManagerScrape.ApplyConfig(c)
	go func() {
		discoveryManagerScrape.Run()
	}()

	go func() {
		scrapeManager.Run(discoveryManagerScrape.SyncCh())
	}()
}
