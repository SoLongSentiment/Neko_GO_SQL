package nekosql

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"
)

type Client struct {
	conn    net.Conn
	decoder *json.Decoder
	encoder *json.Encoder
	writer  *bufio.Writer
	mu      sync.Mutex
}

func Dial(addr string, timeout time.Duration) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	return &Client{
		conn:    conn,
		decoder: json.NewDecoder(reader),
		encoder: json.NewEncoder(writer),
		writer:  writer,
	}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) Exec(ctx context.Context, sql string) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetDeadline(deadline)
	} else {
		_ = c.conn.SetDeadline(time.Time{})
	}

	if err := c.encoder.Encode(request{SQL: sql}); err != nil {
		return Result{}, err
	}
	if err := c.writer.Flush(); err != nil {
		return Result{}, err
	}

	resp := response{}
	if err := c.decoder.Decode(&resp); err != nil {
		return Result{}, err
	}
	if resp.Error != "" {
		switch resp.Error {
		case ErrConflict.Error():
			return Result{}, ErrConflict
		case ErrNoTransaction.Error():
			return Result{}, ErrNoTransaction
		case ErrTransactionOpen.Error():
			return Result{}, ErrTransactionOpen
		default:
			return Result{}, errors.New(resp.Error)
		}
	}
	return resp.Result, nil
}

func (c *Client) RetryTx(ctx context.Context, attempts int, backoff time.Duration, fn func(context.Context, *Client) error) error {
	return Retry(ctx, attempts, backoff, func() error {
		if _, err := c.Exec(ctx, "BEGIN"); err != nil {
			return err
		}
		if err := fn(ctx, c); err != nil {
			_, _ = c.Exec(context.Background(), "ROLLBACK")
			return err
		}
		_, err := c.Exec(ctx, "COMMIT")
		return err
	})
}
