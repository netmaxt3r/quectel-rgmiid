package main

import (
	"context"
	"flag"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"rgmii/daemon"
	"rgmii/mqtt"
	"rgmii/web"
)

func main() {
	modemAddr := flag.String("modem", getEnv("MODEM_ADDR", "192.168.225.1:1555"), "Modem RGMII AT TCP address (IP:port)")
	webPort := flag.String("port", getEnv("PORT", "8080"), "HTTP Web Server Port")
	pollInterval := flag.Int("interval", getEnvInt("POLL_INTERVAL", 10), "Background polling interval in seconds")
	authUser := flag.String("user", getEnv("AUTH_USER", ""), "Web Auth Username (default: disabled)")
	authPass := flag.String("pass", getEnv("AUTH_PASS", ""), "Web Auth Password (default: disabled)")
	apiKey := flag.String("key", getEnv("AUTH_KEY", ""), "Static API Key for external tools (default: disabled)")
	sessionDuration := flag.Duration("session-duration", getEnvDuration("SESSION_DURATION", 24*time.Hour), "Web session duration")
	dataDir := flag.String("data-dir", getEnv("DATA_DIR", "."), "Data directory for persistent files (e.g. sessions.json)")

	mqttServer := flag.String("mqtt-server", getEnv("MQTT_SERVER", ""), "MQTT Broker Server URL (e.g. tcp://192.168.1.10:1883) (default: disabled)")
	mqttUser := flag.String("mqtt-user", getEnv("MQTT_USER", ""), "MQTT Username (default: empty)")
	mqttPass := flag.String("mqtt-pass", getEnv("MQTT_PASS", ""), "MQTT Password (default: empty)")
	mqttTopic := flag.String("mqtt-topic", getEnv("MQTT_TOPIC", "rgmii"), "MQTT base topic for status (default: rgmii)")
	mqttDiscovery := flag.Bool("mqtt-discovery", getEnvBool("MQTT_DISCOVERY", true), "Enable Home Assistant MQTT Auto Discovery (default: true)")
	mqttDiscoveryPrefix := flag.String("mqtt-discovery-prefix", getEnv("MQTT_DISCOVERY_PREFIX", "homeassistant"), "Home Assistant Auto Discovery prefix (default: homeassistant)")
	logFormat := flag.String("log-format", getEnv("LOG_FORMAT", "text"), "Log format (text or json)")
	atiDebug := flag.Bool("ati-debug", getEnvBool("ATI_DEBUG", false), "Log all raw AT commands and responses in real time")

	flag.Parse()

	if *pollInterval <= 0 {
		slog.Error("Invalid poll interval configured (must be greater than 0)", "interval", *pollInterval)
		os.Exit(1)
	}

	// Initialize slog to write to stdout and display local time (respecting TZ)
	logLevel := slog.LevelInfo
	if *atiDebug {
		logLevel = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{
		Level: logLevel,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && !a.Value.Time().IsZero() {
				return slog.String(slog.TimeKey, a.Value.Time().Local().Format("2006-01-02T15:04:05.000-07:00"))
			}
			return a
		},
	}
	var handler slog.Handler
	if *logFormat == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(handler))

	slog.Info("Starting Quectel RGMII Daemon")
	slog.Info("Configuration",
		"modem_addr", *modemAddr,
		"web_port", *webPort,
		"poll_interval_seconds", *pollInterval,
	)

	if *authUser != "" && *authPass != "" {
		slog.Info("Web authentication status", "enabled", true, "user", *authUser)
	} else {
		slog.Info("Web authentication status", "enabled", false)
	}
	slog.Info("API key access status", "enabled", *apiKey != "")

	if *mqttServer != "" {
		logServer := *mqttServer
		u, err := url.Parse(*mqttServer)
		if err == nil && u.User != nil {
			if _, hasPass := u.User.Password(); hasPass {
				u.User = url.UserPassword(u.User.Username(), "xxxxx")
				logServer = u.String()
			}
		}
		slog.Info("MQTT service status",
			"enabled", true,
			"server", logServer,
			"topic_base", *mqttTopic,
			"ha_discovery", *mqttDiscovery,
			"discovery_prefix", *mqttDiscoveryPrefix,
		)
	} else {
		slog.Info("MQTT service status", "enabled", false)
	}

	// Initialize daemon
	d := daemon.NewDaemon(*modemAddr, time.Duration(*pollInterval)*time.Second)
	d.SetATIDebug(*atiDebug)

	// Create cancelable context for graceful daemon shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Configure MQTT if active
	var mqttClient *mqtt.Client
	if *mqttServer != "" {
		var err error
		mqttClient, err = mqtt.NewClient(mqtt.Config{
			Server:          *mqttServer,
			Username:        *mqttUser,
			Password:        *mqttPass,
			Topic:           *mqttTopic,
			Discovery:       *mqttDiscovery,
			DiscoveryPrefix: *mqttDiscoveryPrefix,
			ModemAddr:       *modemAddr,
		})
		if err != nil {
			slog.Error("Failed to initialize MQTT client", "error", err)
			os.Exit(1)
		}

		if err := mqttClient.Connect(ctx); err != nil {
			slog.Warn("MQTT initial connection attempt failed, will auto-retry in background", "error", err)
		}

		// Register the status publisher callback with the daemon
		d.OnStatusUpdate(mqttClient.PublishStatus)
	}

	// Start background poller
	go d.Start(ctx)

	// Intercept terminate/interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start web dashboard
	srv := web.NewServer(d, *modemAddr, *authUser, *authPass, *apiKey)
	srv.SetSessionDuration(*sessionDuration)
	if *dataDir != "" {
		srv.SetDataDir(*dataDir)
	}
	go func() {
		if err := srv.Start(*webPort); err != nil && err.Error() != "http: Server closed" {
			slog.Error("Web server crashed", "error", err)
			sigChan <- syscall.SIGTERM
		}
	}()

	<-sigChan

	slog.Info("Shutting down gracefully...")
	cancel()

	// Gracefully shut down the HTTP server with a deadline
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server shutdown error", "error", err)
	}

	if mqttClient != nil {
		slog.Info("Disconnecting MQTT client...")
		mqttClient.Disconnect()
	}

	slog.Info("Daemon terminated.")
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if valStr, ok := os.LookupEnv(key); ok {
		if val, err := strconv.Atoi(valStr); err == nil {
			return val
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if valStr, ok := os.LookupEnv(key); ok {
		if val, err := strconv.ParseBool(valStr); err == nil {
			return val
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if valStr, ok := os.LookupEnv(key); ok {
		if val, err := time.ParseDuration(valStr); err == nil {
			return val
		}
	}
	return fallback
}
