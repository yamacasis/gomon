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

	notifiers := buildNotifiers(cfg)
	log.Printf("starting gomon — monitoring %d site(s), %d notifier(s)", len(cfg.Websites), len(notifiers))

	for _, site := range cfg.Websites {
		site := site
		go runMonitor(site, notifiers, logger)
	}

	go runHeartbeat(cfg, notifiers, logger)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down")
}
