package nekosql

import (
	"context"
	"net"
	"time"
)

type BenchScenario struct {
	Name      string        `json:"name"`
	Ops       int64         `json:"ops"`
	Duration  time.Duration `json:"duration"`
	OpsPerSec float64       `json:"ops_per_sec"`
}

type BenchReport struct {
	Module    string          `json:"module"`
	StartedAt time.Time       `json:"started_at"`
	Duration  time.Duration   `json:"duration"`
	Results   []BenchScenario `json:"results"`
}

func RunBenchmarks(duration time.Duration) (BenchReport, error) {
	if duration <= 0 {
		duration = time.Second
	}
	report := BenchReport{
		Module:    "neko_sql",
		StartedAt: time.Now().UTC(),
		Duration:  duration,
		Results:   make([]BenchScenario, 0, 2),
	}

	engine := NewEngine()
	if err := engine.ApplyMigrations(DemoMigrations()); err != nil {
		return BenchReport{}, err
	}
	start := time.Now()
	var ops int64
	id := int64(1)
	for time.Since(start) < duration {
		_, err := engine.Exec("INSERT INTO players (id, name, mmr) VALUES (" + strconvFormatInt(id) + ", 'bench', 1000)")
		if err == nil {
			ops++
			id++
		}
	}
	elapsed := time.Since(start)
	report.Results = append(report.Results, BenchScenario{Name: "engine_insert", Ops: ops, Duration: elapsed, OpsPerSec: float64(ops) / elapsed.Seconds()})

	selectEngine := NewEngine()
	if err := selectEngine.ApplyMigrations(DemoMigrations()); err != nil {
		return BenchReport{}, err
	}
	if _, err := selectEngine.Exec("INSERT INTO players (id, name, mmr) VALUES (1, 'bench', 1000)"); err != nil {
		return BenchReport{}, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return BenchReport{}, err
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = NewServer(selectEngine).Serve(ctx, ln)
	}()

	client, err := Dial(ln.Addr().String(), 2*time.Second)
	if err != nil {
		return BenchReport{}, err
	}
	defer client.Close()

	start = time.Now()
	ops = 0
	for time.Since(start) < duration {
		if _, err := client.Exec(context.Background(), "SELECT * FROM players WHERE id = 1"); err != nil {
			break
		}
		ops++
	}
	elapsed = time.Since(start)
	report.Results = append(report.Results, BenchScenario{Name: "tcp_select_roundtrip", Ops: ops, Duration: elapsed, OpsPerSec: float64(ops) / elapsed.Seconds()})
	return report, nil
}
