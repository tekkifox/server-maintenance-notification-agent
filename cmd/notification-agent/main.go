package main

// @title Server Maintenance Notification Agent API
// @version 1.0
// @description HTTP API for Discord maintenance notifications and remote console broadcasts.
// @BasePath /
// @schemes http

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "server-maintenance-notification-agent/docs"
	"server-maintenance-notification-agent/internal/config"
	"server-maintenance-notification-agent/internal/discord"
	"server-maintenance-notification-agent/internal/dockercontrol"
	"server-maintenance-notification-agent/internal/httpapi"
	"server-maintenance-notification-agent/internal/pelican"
	"server-maintenance-notification-agent/internal/rconcontrol"
	"server-maintenance-notification-agent/internal/service"
	"server-maintenance-notification-agent/internal/telnetcontrol"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := config.LoadEnvFile(".env"); err != nil {
		log.Fatalf("load env file: %v", err)
	}

	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	discordClient, err := discord.NewClient(cfg.DiscordBotToken)
	if err != nil {
		log.Fatalf("create discord client: %v", err)
	}
	defer func() {
		if closeErr := discordClient.Close(); closeErr != nil {
			log.Printf("close discord client: %v", closeErr)
		}
	}()

	if err := discordClient.Open(); err != nil {
		log.Fatalf("open discord client: %v", err)
	}

	dockerClient, err := dockercontrol.NewClient()
	if err != nil {
		log.Printf("docker control unavailable: %v", err)
	}
	if dockerClient != nil {
		defer func() {
			if closeErr := dockerClient.Close(); closeErr != nil {
				log.Printf("close docker client: %v", closeErr)
			}
		}()
	}

	notifier := service.NewNotifier(discordClient, cfg.DefaultChannelIDs)
	rconClient := rconcontrol.NewClient()
	telnetClient := telnetcontrol.NewClient()
	dockerCommander := service.NewDockerCommander(
		dockerClient,
		rconClient,
		telnetClient,
		cfg.DefaultDockerContainerIDs,
		cfg.DefaultDockerContainerNames,
		cfg.DefaultRCONContainerNames,
		cfg.DefaultRCONContainerTransports,
		cfg.DefaultRCONContainerAddresses,
		cfg.DefaultRCONContainerPasswords,
	)
	dockerCommander.SetDockerFallbackGameTypes(cfg.DefaultDockerFallbackGameTypes)
	if cfg.PelicanAPIURL != "" && cfg.PelicanAPIToken != "" {
		resolver, err := pelican.NewResolver(cfg.PelicanAPIURL, cfg.PelicanAPIToken, cfg.PelicanLogAPIOutput)
		if err != nil {
			log.Printf("pelican game resolver unavailable: %v", err)
		} else {
			dockerCommander.SetGameResolver(resolver)
		}
	}
	bridge := service.NewDiscordRCONBridge(discordClient.Session(), dockerCommander, cfg.DiscordIngestChannelIDs, cfg.RCONIngestContainerNames, cfg.DiscordBridgeFilteredPhrases)
	bridge.Start()

	server := httpapi.NewServer(notifier, dockerCommander)
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", cfg.HTTPAddr)
		if serveErr := httpServer.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("shutdown http server: %v", err)
		}
	case err := <-errCh:
		if err != nil {
			log.Fatalf("serve http: %v", err)
		}
	}

	log.Println("shutdown complete")
}
