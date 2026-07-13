// wscommuter is a standalone client that simulates a commuter's app
// watching live vehicles on a route over /ws/commuter. Pair with
// cmd/wsdriver to see Stage 4's live flow end-to-end without a browser.
//
// Usage:
//
//	go run ./cmd/wscommuter -route <route-id> [-server localhost:8080]
//
// Prints the initial snapshot, then every update/offline event as it
// arrives. Ctrl+C to exit.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/websocket"
)

func main() {
	routeID := flag.String("route", "", "route_id to watch")
	server := flag.String("server", "localhost:8080", "backend host:port")
	flag.Parse()

	if *routeID == "" {
		fmt.Fprintln(os.Stderr, "usage: wscommuter -route <route-id> [-server host:port]")
		os.Exit(1)
	}

	q := url.Values{"route_id": {*routeID}}
	u := "ws://" + *server + "/ws/commuter?" + q.Encode()

	conn, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		if resp != nil {
			log.Fatalf("dial failed (HTTP %d): %v", resp.StatusCode, err)
		}
		log.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()
	log.Printf("subscribed to route %s", *routeID)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				log.Printf("connection closed: %v", err)
				return
			}
			log.Printf("event: %+v", msg)
		}
	}()

	select {
	case <-ctx.Done():
	case <-done:
	}
}
