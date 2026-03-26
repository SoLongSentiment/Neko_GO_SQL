package nekosql

import (
	"context"
	"net"
	"testing"
	"time"
)

func BenchmarkNekoSQL(b *testing.B) {
	b.Run("engine_insert", func(b *testing.B) {
		engine := NewEngine()
		if err := engine.ApplyMigrations(DemoMigrations()); err != nil {
			b.Fatalf("apply migrations: %v", err)
		}
		b.ReportAllocs()
		start := time.Now()
		for i := 0; i < b.N; i++ {
			_, err := engine.Exec("INSERT INTO players (id, name, mmr) VALUES (" + strconvFormatInt(int64(i+1)) + ", 'bench', 1000)")
			if err != nil {
				b.Fatalf("insert: %v", err)
			}
		}
		elapsed := time.Since(start)
		b.ReportMetric(float64(b.N)/elapsed.Seconds(), "insert/s")
	})

	b.Run("tcp_select_roundtrip", func(b *testing.B) {
		engine := NewEngine()
		if err := engine.ApplyMigrations(DemoMigrations()); err != nil {
			b.Fatalf("apply migrations: %v", err)
		}
		if _, err := engine.Exec("INSERT INTO players (id, name, mmr) VALUES (1, 'bench', 1000)"); err != nil {
			b.Fatalf("seed: %v", err)
		}

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			b.Fatalf("listen: %v", err)
		}
		defer ln.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			_ = NewServer(engine).Serve(ctx, ln)
		}()

		client, err := Dial(ln.Addr().String(), 2*time.Second)
		if err != nil {
			b.Fatalf("dial: %v", err)
		}
		defer client.Close()

		b.ReportAllocs()
		start := time.Now()
		for i := 0; i < b.N; i++ {
			if _, err := client.Exec(context.Background(), "SELECT * FROM players WHERE id = 1"); err != nil {
				b.Fatalf("select: %v", err)
			}
		}
		elapsed := time.Since(start)
		b.ReportMetric(float64(b.N)/elapsed.Seconds(), "roundtrip/s")
	})
}
