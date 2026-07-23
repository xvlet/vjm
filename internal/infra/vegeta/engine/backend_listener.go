package engine

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/xvlet/vjm/internal/domain"
)

func startBackendListeners(plan *domain.TestPlan) chan<- BackendMetrics {
	if len(plan.BackendListeners) == 0 {
		return nil
	}

	metricsChan := make(chan BackendMetrics, 100)

	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		for metrics := range metricsChan {
			for _, bl := range plan.BackendListeners {
				if strings.Contains(bl.Classname, "influxdb") {
					sendInfluxDBMetrics(client, bl, metrics)
				}
			}
		}
	}()

	return metricsChan
}

type BackendMetrics struct {
	Timestamp time.Time
	TgName    string
	TotalReqs int64
	OkReqs    int64
	KoReqs    int64
	AvgLat    float64
	P99Lat    float64
	MaxLat    float64
}

func sendInfluxDBMetrics(client *http.Client, bl *domain.BackendListener, metrics BackendMetrics) {
	url := bl.Arguments["influxdbUrl"]
	if url == "" {
		return
	}
	meas := bl.Arguments["measurement"]
	if meas == "" {
		meas = "jmeter"
	}
	app := bl.Arguments["application"]
	if app == "" {
		app = "vjm"
	}

	// Line protocol: measurement,tags fields timestamp
	// jmeter,application=vjm,transaction=all ok=10,ko=0,avg=12.3,pct99=45.6,max=100.0 1620000000000000000
	line := fmt.Sprintf("%s,application=%s,transaction=all ok=%d,ko=%d,all=%d,avg=%.2f,pct99=%.2f,max=%.2f %d\n",
		meas, app, metrics.OkReqs, metrics.KoReqs, metrics.TotalReqs, metrics.AvgLat, metrics.P99Lat, metrics.MaxLat, metrics.Timestamp.UnixNano())

	req, err := http.NewRequest("POST", url, bytes.NewBufferString(line))
	if err != nil {
		return
	}

	// Check for InfluxDB v2 token
	token := bl.Arguments["influxdbToken"]
	if token != "" {
		req.Header.Set("Authorization", "Token "+token)
	}

	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
}
