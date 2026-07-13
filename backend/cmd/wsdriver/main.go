// wsdriver is a standalone client that simulates a driver's app streaming
// position updates over /ws/driver. It's a manual, human-driven way to see
// Stage 4's live flow end-to-end without a browser — see docs/PROGRESS.md
// for a full walkthrough (login → this → cmd/wscommuter).
//
// Usage:
//
//	go run ./cmd/wsdriver -token <driver-jwt> -route <route-id> [-server localhost:8080] [-lat -33.9] [-lng 18.4] [-seats 3]
//
// Once connected it streams small simulated position deltas every 2s, and
// applies one seat delta at startup if -seats is set. Ctrl+C to disconnect
// (the server marks the vehicle offline the moment this process exits).
//
// Stage 6: /ws/driver is now bidirectional — this client also reads frames
// concurrently and prints any server-pushed stop-request alert as it
// arrives.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	token := flag.String("token", "", "driver JWT (from POST /auth/login)")
	routeID := flag.String("route", "", "route_id to go online on")
	server := flag.String("server", "localhost:8080", "backend host:port")
	lat := flag.Float64("lat", -33.9249, "starting latitude")
	lng := flag.Float64("lng", 18.4241, "starting longitude")
	seatsDelta := flag.Int("seats", 0, "optional one-time seats_delta to send after connecting")
	flag.Parse()

	if *token == "" || *routeID == "" {
		fmt.Fprintln(os.Stderr, "usage: wsdriver -token <driver-jwt> -route <route-id> [-server host:port] [-lat N] [-lng N] [-seats N]")
		os.Exit(1)
	}

	q := url.Values{"route_id": {*routeID}}
	u := "ws://" + *server + "/ws/driver?" + q.Encode()
	header := http.Header{"Authorization": {"Bearer " + *token}}

	conn, resp, err := websocket.DefaultDialer.Dial(u, header)
	if err != nil {
		if resp != nil {
			log.Fatalf("dial failed (HTTP %d): %v", resp.StatusCode, err)
		}
		log.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()
	log.Printf("connected as driver, online on route %s", *routeID)

	// Read loop: prints any server-pushed message (currently just Stage 6's
	// stop-request alerts) as it arrives. Runs concurrently with the write
	// loop below, satisfying gorilla/websocket's one-reader/one-writer rule.
	go func() {
		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return // connection closed
			}
			b, _ := json.Marshal(msg)
			log.Printf("ALERT received: %s", b)
		}
	}()

	if *seatsDelta != 0 {
		if err := conn.WriteJSON(map[string]any{"seats_delta": *seatsDelta}); err != nil {
			log.Printf("failed to send seats_delta: %v", err)
		} else {
			log.Printf("sent seats_delta=%d", *seatsDelta)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	curLat, curLng := *lat, *lng
	for {
		select {
		case <-ctx.Done():
			log.Println("shutting down, going offline")
			return
		case <-ticker.C:
			curLat += (rand.Float64() - 0.5) * 0.001
			curLng += (rand.Float64() - 0.5) * 0.001
			msg := map[string]any{"lat": curLat, "lng": curLng}
			if err := conn.WriteJSON(msg); err != nil {
				log.Printf("write failed: %v", err)
				return
			}
			b, _ := json.Marshal(msg)
			log.Printf("sent position update: %s", b)
		}
	}
}
