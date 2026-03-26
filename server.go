package nekosql

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
)

type Server struct {
	engine *Engine
}

func NewServer(engine *Engine) *Server {
	if engine == nil {
		engine = NewEngine()
	}
	return &Server{engine: engine}
}

type request struct {
	SQL string `json:"sql"`
}

type response struct {
	OK     bool              `json:"ok"`
	Result Result            `json:"result,omitempty"`
	Error  string            `json:"error,omitempty"`
	State  map[string]string `json:"state,omitempty"`
}

type session struct {
	tx *Tx
}

func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer conn.Close()
			s.serveConn(conn)
		}()
	}
}

func (s *Server) serveConn(conn net.Conn) {
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	decoder := json.NewDecoder(reader)
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	sess := &session{}

	for {
		var req request
		if err := decoder.Decode(&req); err != nil {
			return
		}
		resp := s.handleSQL(sess, req.SQL)
		if err := encoder.Encode(resp); err != nil {
			return
		}
		if err := writer.Flush(); err != nil {
			return
		}
	}
}

func (s *Server) handleSQL(sess *session, sql string) response {
	switch strings.ToUpper(strings.TrimSpace(strings.TrimSuffix(sql, ";"))) {
	case "BEGIN":
		if sess.tx != nil {
			return response{Error: ErrTransactionOpen.Error()}
		}
		sess.tx = s.engine.Begin()
		return response{OK: true, State: map[string]string{"tx": "open"}}
	case "COMMIT":
		if sess.tx == nil {
			return response{Error: ErrNoTransaction.Error()}
		}
		err := sess.tx.Commit()
		sess.tx = nil
		if err != nil {
			return response{Error: err.Error()}
		}
		return response{OK: true, State: map[string]string{"tx": "committed"}}
	case "ROLLBACK":
		if sess.tx == nil {
			return response{Error: ErrNoTransaction.Error()}
		}
		err := sess.tx.Rollback()
		sess.tx = nil
		if err != nil {
			return response{Error: err.Error()}
		}
		return response{OK: true, State: map[string]string{"tx": "rolled_back"}}
	}

	var (
		result Result
		err    error
	)
	if sess.tx != nil {
		result, err = sess.tx.Exec(sql)
	} else {
		result, err = s.engine.Exec(sql)
	}
	if err != nil {
		return response{Error: err.Error()}
	}
	return response{OK: true, Result: result}
}
