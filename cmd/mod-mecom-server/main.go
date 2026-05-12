// mod-mecom-server exposes a TCP MeCom Device Server-like access point backed
// by one downstream MeCom transport. It serializes multiple client requests so
// tools and Loom modules can coexist without racing the same COM/TCP bus.
//
// Configuration:
//
//	LOOM_MECOM_SERVER_LISTEN          TCP listen address, default 127.0.0.1:50100
//	LOOM_MECOM_SERVER_TARGET          downstream target, e.g. 127.0.0.1:50000 or serial:COM3@57600
//	LOOM_MECOM_SERVER_REQUEST_TIMEOUT per-request timeout, default 2s
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/egidinas/loom-gossamer-shared/go/ipc"
	"github.com/egidinas/loom-gossamer-shared/go/lifecycle"
	"github.com/egidinas/loom-gossamer-shared/go/otelutil"
	"github.com/egidinas/loom-gossamer-shared/go/safego"
	"github.com/egidinas/meerstetter-go/mecomserver"
)

const moduleID = "mod-mecom-server"

type appConfig struct {
	ListenAddr string
	Server     mecomserver.Config
}

func main() {
	shutdownOTel, _ := otelutil.SetupOTel("mod-mecom-server", os.Stdout)
	defer shutdownOTel(context.Background())
	cfg, err := parseConfig(os.Args[1:], os.Getenv)
	if err != nil {
		slog.Error(fmt.Sprintf("[%s] config: %v", moduleID, err))
		os.Exit(1)
	}

	instanceID := envOr(os.Getenv, "LOOM_INSTANCE_ID", "local")
	bus, err := ipc.NewBus("", "ipc-"+moduleID+"-"+instanceID, "")
	if err != nil {
		slog.Error(fmt.Sprintf("[%s] IPC: %v", moduleID, err))
		os.Exit(1)
	}
	defer bus.Close()
	_ = bus.PublishLifecycle(moduleID, instanceID, lifecycle.StateStarting,
		fmt.Sprintf("listen=%s target=%s", cfg.ListenAddr, cfg.Server.Target))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg.Server.Logger = log.New(os.Stdout, "["+moduleID+"] ", log.LstdFlags|log.Lmicroseconds)
	errCh := make(chan error, 1)
	go func() {
		defer safego.Recover("background_routine")
		errCh <- mecomserver.ListenAndServe(ctx, cfg.ListenAddr, cfg.Server)
	}()

	_ = bus.PublishLifecycle(moduleID, instanceID, lifecycle.StateReady, "serving")
	lifecycle.WatchSupervisor(bus.Connection(), 5*time.Second)
	_ = bus.SubscribeDrain(moduleID, instanceID, func() {
		_ = bus.PublishDrained(moduleID, instanceID, lifecycle.HandoverToken{})
		cancel()
	})

	select {
	case err := <-errCh:
		if err != nil && ctx.Err() == nil {
			_ = bus.PublishLifecycle(moduleID, instanceID, lifecycle.StateStopped, err.Error())
			slog.Error(fmt.Sprintf("[%s] server failed: %v", moduleID, err))
			os.Exit(1)
		}
	case <-ctx.Done():
	}

	_ = bus.PublishLifecycle(moduleID, instanceID, lifecycle.StateDraining, "")
	time.Sleep(200 * time.Millisecond)
	_ = bus.PublishLifecycle(moduleID, instanceID, lifecycle.StateStopped, "stopped")
}

func parseConfig(args []string, getenv func(string) string) (appConfig, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	fs := flag.NewFlagSet("mod-mecom-server", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	listenDefault := envOr(getenv, "LOOM_MECOM_SERVER_LISTEN", "127.0.0.1:50100")
	targetDefault := envOr(getenv, "LOOM_MECOM_SERVER_TARGET", "")
	timeoutDefault := envOr(getenv, "LOOM_MECOM_SERVER_REQUEST_TIMEOUT", "2s")

	listen := fs.String("listen", listenDefault, "TCP listen address exposed to MeCom clients")
	target := fs.String("target", targetDefault, "Downstream MeCom target, for example 127.0.0.1:50000 or serial:COM3@57600")
	requestTimeoutName := fs.String("request-timeout", timeoutDefault, "Per-request downstream timeout")

	if err := fs.Parse(args); err != nil {
		return appConfig{}, err
	}

	listenAddr := strings.TrimSpace(*listen)
	if listenAddr == "" {
		return appConfig{}, fmt.Errorf("-listen or LOOM_MECOM_SERVER_LISTEN is required")
	}
	targetAddr := strings.TrimSpace(*target)
	if targetAddr == "" {
		return appConfig{}, fmt.Errorf("-target or LOOM_MECOM_SERVER_TARGET is required")
	}
	requestTimeout, err := time.ParseDuration(strings.TrimSpace(*requestTimeoutName))
	if err != nil {
		return appConfig{}, fmt.Errorf("invalid -request-timeout: %w", err)
	}

	return appConfig{
		ListenAddr: listenAddr,
		Server: mecomserver.Config{
			Target:         targetAddr,
			RequestTimeout: requestTimeout,
		},
	}, nil
}

func envOr(getenv func(string) string, key, fallback string) string {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		return v
	}
	return fallback
}
