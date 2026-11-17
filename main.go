package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	logger, err := newLogger(cfg.LogFile)
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	defer logger.close()

	notifiers, err := buildNotifiers(cfg)
	if err != nil {
		log.Fatalf("notifiers: %v", err)
	}
	notifications, err := newNotificationDispatcher(cfg.Notifications, notifiers)
	if err != nil {
		log.Fatalf("notification queue: %v", err)
	}
	defer notifications.close()
	metrics := newMonitorMetrics()
	metricsServer, err := startMetricsServer(cfg.Metrics, metrics)
	if err != nil {
		log.Fatalf("metrics server: %v", err)
	}
	defer stopMetricsServer(metricsServer)

	log.Printf("starting gomon — monitoring %d site(s), %d prometheus target(s), %d notifier(s)",
		len(cfg.Websites), len(cfg.PrometheusTargets), len(notifiers))

	for _, site := range cfg.Websites {
		site := site
		go runMonitor(site, notifications, logger, metrics)
	}
	for _, target := range cfg.PrometheusTargets {
		target := target
		go runPrometheusTarget(target, notifications, metrics)
	}

	go runHeartbeat(cfg, notifications, logger)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down")
}
