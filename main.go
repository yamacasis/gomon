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

	tg := newTelegram(cfg.Telegram.BotToken, cfg.Telegram.ChatID)

	log.Printf("starting gomon — monitoring %d site(s)", len(cfg.Websites))

	for _, site := range cfg.Websites {
		site := site // capture loop variable
		go runMonitor(site, tg, logger)
	}

	go runHeartbeat(cfg, tg, logger)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down")
}
