package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/egidinas/meerstetter-go/utility"
)

func main() {
	configPath := flag.String("config", "", "path to JSON config")
	printDefault := flag.Bool("print-default-config", false, "print a default JSON config and exit")
	flag.Parse()

	if *printDefault {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(utility.DefaultConfig()); err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg, err := utility.LoadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	server, err := utility.NewServer(cfg)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(os.Stderr, "meerstetterd listening on http://%s\n", cfg.HTTPListen)
	if err := server.ListenAndServe(ctx); err != nil {
		log.Fatal(err)
	}
}
