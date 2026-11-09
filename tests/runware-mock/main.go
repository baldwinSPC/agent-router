// Copyright Envoy AI Gateway Authors
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package main

import (
	"flag"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/envoyproxy/ai-gateway/tests/internal/testrunware"
)

func main() {
	port := flag.Int("port", 11434, "Loopback port for the synthetic backend")
	flag.Parse()
	server := &http.Server{
		Addr:              "127.0.0.1:" + strconv.Itoa(*port),
		Handler:           &testrunware.Handler{},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	log.Printf("Synthetic Runware-compatible backend at http://%s; no live API access", server.Addr)
	log.Fatal(server.ListenAndServe())
}
