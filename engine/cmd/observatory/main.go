// Observatory adds a loopback dashboard and Ollama measurement proxy to the upstream engine.
package main

import (
	"flag"
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/semyonfox/tokentelemetry/engine/internal/cli"
	"github.com/semyonfox/tokentelemetry/engine/internal/observatory"
)

func main() {
	if len(os.Args)>1 && os.Args[1]=="collect" {os.Setenv("TT_OFFLINE","1");ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second);defer cancel();c:=observatory.Collect(ctx);json.NewEncoder(os.Stdout).Encode(c);if c.Error!=""{os.Exit(1)};return}
	if len(os.Args) > 1 && os.Args[1] == "accounting" {
		os.Exit(cli.Main(append([]string{"tokentelemetry"}, os.Args[2:]...)))
	}
	base, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	dir := flag.String("data-dir", filepath.Join(base, "TokenTelemetryObservatory"), "configuration and measured counters directory")
	port := flag.Int("port", 8787, "loopback dashboard and Ollama proxy port")
	flag.Parse()
	if err := observatory.Run(*dir, *port); err != nil {
		log.Fatal(err)
	}
}
