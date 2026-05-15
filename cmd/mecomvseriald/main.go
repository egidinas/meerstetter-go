// mecomvseriald exposes one LAN TCP listener for addressed MeCom serial
// devices and routes each request by the device address in the MeCom frame.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/egidinas/meerstetter-go/mecomserver"
)

type routeFlags []mecomserver.Route

func (r *routeFlags) String() string {
	parts := make([]string, 0, len(*r))
	for _, route := range *r {
		parts = append(parts, fmt.Sprintf("0x%02X=%s", route.Address, route.Target))
	}
	return strings.Join(parts, ",")
}

func (r *routeFlags) Set(v string) error {
	eq := strings.Index(v, "=")
	if eq < 0 {
		return fmt.Errorf("route must be address=target")
	}
	addrText := strings.TrimSpace(v[:eq])
	target := strings.TrimSpace(v[eq+1:])
	if target == "" {
		return fmt.Errorf("route target required")
	}
	addr, err := parseAddress(addrText)
	if err != nil {
		return err
	}
	*r = append(*r, mecomserver.Route{Address: addr, Target: target})
	return nil
}

func main() {
	var routes routeFlags
	listen := flag.String("listen", "127.0.0.1:50000", "TCP listen address")
	timeout := flag.Duration("timeout", 2*time.Second, "per-request downstream timeout")
	reconnectDelay := flag.Duration("reconnect-delay", 500*time.Millisecond, "delay after downstream dial failures")
	flag.Var(&routes, "route", "address=target route; repeatable, e.g. -route 75=serial:/dev/ttyUSB0@57600")
	flag.Parse()

	if len(routes) == 0 {
		fmt.Fprintln(os.Stderr, "at least one -route is required")
		os.Exit(2)
	}

	logger := log.New(os.Stderr, "mecomvseriald ", log.LstdFlags|log.Lmicroseconds)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Printf("listen=%s routes=%s", *listen, routes.String())
	cfg := &mecomserver.RouterConfig{
		Routes:         routes,
		RequestTimeout: *timeout,
		ReconnectDelay: *reconnectDelay,
		Logger:         logger,
	}
	err := mecomserver.ListenAndServeRouter(ctx, *listen, cfg)
	if err != nil {
		logger.Printf("server failed: %v", err)
		os.Exit(1)
	}
}

func parseAddress(v string) (byte, error) {
	n, err := strconv.ParseUint(v, 0, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid address %q: %w", v, err)
	}
	if n == 0 {
		return 0, fmt.Errorf("address 0 is reserved")
	}
	if n > 254 {
		return 0, fmt.Errorf("address %d outside MeCom 1..254", n)
	}
	return byte(n), nil
}
