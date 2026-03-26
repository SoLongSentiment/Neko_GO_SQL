package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	nekosql "neko_sql"
)

func main() {
	engine := nekosql.NewEngine()
	if err := engine.ApplyMigrations(nekosql.DemoMigrations()); err != nil {
		log.Fatalf("apply migrations: %v", err)
	}

	addr := ":17401"
	if value := os.Getenv("NEKO_SQL_ADDR"); value != "" {
		addr = value
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("neko_sql listening on %s", addr)
	if err := nekosql.NewServer(engine).Serve(ctx, ln); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
