// Command gdrive-bot is a Go rewrite of the google-drive-telegram-bot
// project: a Telegram bot that mirrors files from direct URLs into
// Google Drive, with Drive browsing, search, copy, move, and delete.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/bot"
	"github.com/zzzhoo1/deployment-automation-framework-8429/internal/config"
)

func main() {
	cfg := config.Load()
	if cfg.BotToken == "" {
		log.Fatal("BOT_TOKEN environment variable is required")
	}

	app, err := bot.New(cfg)
	if err != nil {
		log.Fatalf("init bot: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("starting gdrive-bot (download dir %s, max concurrent %d)",
		cfg.DownloadDirectory, cfg.MaxConcurrentMirrors)
	if err := app.Run(ctx); err != nil {
		log.Fatalf("run: %v", err)
	}
	log.Println("stopped")
}
