package nekosql

import (
	"context"
	"net"
	"testing"
	"time"
)

func startTestServer(tb testing.TB) (*Engine, *Client, func()) {
	tb.Helper()

	engine := NewEngine()
	if err := engine.ApplyMigrations(DemoMigrations()); err != nil {
		tb.Fatalf("apply migrations: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = NewServer(engine).Serve(ctx, ln)
	}()

	client, err := Dial(ln.Addr().String(), 2*time.Second)
	if err != nil {
		tb.Fatalf("dial: %v", err)
	}

	cleanup := func() {
		_ = client.Close()
		cancel()
		_ = ln.Close()
	}
	return engine, client, cleanup
}

func TestCreateInsertSelect(t *testing.T) {
	_, client, cleanup := startTestServer(t)
	defer cleanup()

	if _, err := client.Exec(context.Background(), "INSERT INTO players (id, name, mmr) VALUES (1, 'alice', 1000)"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	result, err := client.Exec(context.Background(), "SELECT * FROM players WHERE id = 1")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	if result.Rows[0]["name"] != "alice" {
		t.Fatalf("unexpected row: %#v", result.Rows[0])
	}
}

func TestTransactionRollback(t *testing.T) {
	_, client, cleanup := startTestServer(t)
	defer cleanup()

	if _, err := client.Exec(context.Background(), "BEGIN"); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := client.Exec(context.Background(), "INSERT INTO players (id, name, mmr) VALUES (2, 'bob', 1200)"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := client.Exec(context.Background(), "ROLLBACK"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	result, err := client.Exec(context.Background(), "SELECT * FROM players WHERE id = 2")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(result.Rows) != 0 {
		t.Fatalf("expected 0 rows, got %#v", result.Rows)
	}
}

func TestRetryTxOnConflict(t *testing.T) {
	engine, client, cleanup := startTestServer(t)
	defer cleanup()

	if _, err := engine.Exec("INSERT INTO players (id, name, mmr) VALUES (1, 'alice', 1000)"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tx1 := engine.Begin()
	tx2 := engine.Begin()
	if _, err := tx1.Exec("UPDATE players SET mmr = 1100 WHERE id = 1"); err != nil {
		t.Fatalf("tx1 update: %v", err)
	}
	if _, err := tx2.Exec("UPDATE players SET mmr = 1200 WHERE id = 1"); err != nil {
		t.Fatalf("tx2 update: %v", err)
	}
	if err := tx1.Commit(); err != nil {
		t.Fatalf("tx1 commit: %v", err)
	}
	if err := tx2.Commit(); err != ErrConflict {
		t.Fatalf("expected conflict, got %v", err)
	}

	err := client.RetryTx(context.Background(), 3, time.Millisecond, func(ctx context.Context, txClient *Client) error {
		_, err := txClient.Exec(ctx, "UPDATE players SET mmr = 1300 WHERE id = 1")
		return err
	})
	if err != nil {
		t.Fatalf("retry tx: %v", err)
	}

	result, err := client.Exec(context.Background(), "SELECT * FROM players WHERE id = 1")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("unexpected row after retry: %#v", result.Rows)
	}
	if got := result.Rows[0]["mmr"]; got != float64(1300) && got != int64(1300) && got != 1300 {
		t.Fatalf("unexpected mmr after retry: %#v", result.Rows)
	}
}
