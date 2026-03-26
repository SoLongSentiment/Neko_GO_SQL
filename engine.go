package nekosql

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	ErrConflict        = errors.New("transaction conflict")
	ErrNoTransaction   = errors.New("no active transaction")
	ErrTransactionOpen = errors.New("transaction already open")
)

type Column struct {
	Name       string
	Type       ColumnType
	PrimaryKey bool
}

type Row map[string]Value

func (r Row) clone() Row {
	out := make(Row, len(r))
	for k, v := range r {
		out[k] = v
	}
	return out
}

type Table struct {
	Name       string
	Columns    []Column
	PrimaryKey string
	Rows       map[string]Row
}

func (t *Table) clone() *Table {
	cp := &Table{
		Name:       t.Name,
		Columns:    append([]Column(nil), t.Columns...),
		PrimaryKey: t.PrimaryKey,
		Rows:       make(map[string]Row, len(t.Rows)),
	}
	for k, row := range t.Rows {
		cp.Rows[k] = row.clone()
	}
	return cp
}

func (t *Table) column(name string) (Column, bool) {
	for _, col := range t.Columns {
		if strings.EqualFold(col.Name, name) {
			return col, true
		}
	}
	return Column{}, false
}

type Result struct {
	Columns  []string         `json:"columns,omitempty"`
	Rows     []map[string]any `json:"rows,omitempty"`
	Affected int              `json:"affected,omitempty"`
	Message  string           `json:"message,omitempty"`
}

type state struct {
	tables map[string]*Table
}

func newState() state {
	return state{tables: make(map[string]*Table)}
}

func (s state) clone() state {
	out := newState()
	for name, table := range s.tables {
		out.tables[name] = table.clone()
	}
	return out
}

type Engine struct {
	mu              sync.RWMutex
	current         state
	version         uint64
	appliedVersions map[int]Migration
}

func NewEngine() *Engine {
	return &Engine{
		current:         newState(),
		appliedVersions: make(map[int]Migration),
	}
}

func (e *Engine) Begin() *Tx {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return &Tx{
		engine:       e,
		working:      e.current.clone(),
		startVersion: e.version,
	}
}

func (e *Engine) Exec(sql string) (Result, error) {
	stmt, err := Parse(sql)
	if err != nil {
		return Result{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return executeStatement(&e.current, stmt)
}

func (e *Engine) ApplyMigrations(migrations []Migration) error {
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	tx := e.Begin()
	for _, migration := range migrations {
		if _, ok := e.appliedVersions[migration.Version]; ok {
			continue
		}
		if _, err := tx.Exec(migration.SQL); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", migration.Version, migration.Name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, migration := range migrations {
		e.appliedVersions[migration.Version] = migration
	}
	return nil
}

type Tx struct {
	engine       *Engine
	working      state
	startVersion uint64
	closed       bool
}

func (tx *Tx) Exec(sql string) (Result, error) {
	if tx.closed {
		return Result{}, ErrNoTransaction
	}
	stmt, err := Parse(sql)
	if err != nil {
		return Result{}, err
	}
	return executeStatement(&tx.working, stmt)
}

func (tx *Tx) Commit() error {
	if tx.closed {
		return ErrNoTransaction
	}
	tx.engine.mu.Lock()
	defer tx.engine.mu.Unlock()
	if tx.engine.version != tx.startVersion {
		return ErrConflict
	}
	tx.engine.current = tx.working
	tx.engine.version++
	tx.closed = true
	return nil
}

func (tx *Tx) Rollback() error {
	if tx.closed {
		return ErrNoTransaction
	}
	tx.closed = true
	return nil
}

func executeStatement(st *state, stmt Statement) (Result, error) {
	switch s := stmt.(type) {
	case CreateTableStmt:
		return execCreateTable(st, s)
	case InsertStmt:
		return execInsert(st, s)
	case SelectStmt:
		return execSelect(st, s)
	case UpdateStmt:
		return execUpdate(st, s)
	case DeleteStmt:
		return execDelete(st, s)
	default:
		return Result{}, errors.New("unsupported statement")
	}
}

func execCreateTable(st *state, stmt CreateTableStmt) (Result, error) {
	name := strings.ToLower(stmt.Name)
	if _, exists := st.tables[name]; exists {
		return Result{}, fmt.Errorf("table %s already exists", stmt.Name)
	}
	primaryKey := ""
	columns := make([]Column, 0, len(stmt.Columns))
	for _, raw := range stmt.Columns {
		col := Column{Name: strings.ToLower(raw.Name), Type: raw.Type, PrimaryKey: raw.PrimaryKey}
		if col.PrimaryKey {
			if primaryKey != "" {
				return Result{}, errors.New("multiple primary keys are not supported")
			}
			primaryKey = col.Name
		}
		columns = append(columns, col)
	}
	if primaryKey == "" {
		return Result{}, errors.New("primary key is required")
	}
	st.tables[name] = &Table{Name: name, Columns: columns, PrimaryKey: primaryKey, Rows: make(map[string]Row)}
	return Result{Message: "table created"}, nil
}

func execInsert(st *state, stmt InsertStmt) (Result, error) {
	table, err := requireTable(st, stmt.Table)
	if err != nil {
		return Result{}, err
	}
	if len(stmt.Columns) != len(stmt.Values) {
		return Result{}, errors.New("columns and values length mismatch")
	}
	row := make(Row, len(table.Columns))
	for idx, columnName := range stmt.Columns {
		column, ok := table.column(columnName)
		if !ok {
			return Result{}, fmt.Errorf("unknown column %s", columnName)
		}
		value, err := coerceValue(stmt.Values[idx], column.Type)
		if err != nil {
			return Result{}, err
		}
		row[column.Name] = value
	}
	pk, ok := row[table.PrimaryKey]
	if !ok {
		return Result{}, fmt.Errorf("primary key %s is required", table.PrimaryKey)
	}
	key := pk.String()
	if _, exists := table.Rows[key]; exists {
		return Result{}, fmt.Errorf("row %s already exists", key)
	}
	for _, column := range table.Columns {
		if _, ok := row[column.Name]; !ok {
			row[column.Name] = zeroValue(column.Type)
		}
	}
	table.Rows[key] = row
	return Result{Affected: 1}, nil
}

func execSelect(st *state, stmt SelectStmt) (Result, error) {
	table, err := requireTable(st, stmt.Table)
	if err != nil {
		return Result{}, err
	}

	if stmt.Where != nil && strings.EqualFold(stmt.Where.Column, table.PrimaryKey) {
		return selectByPrimaryKey(table, stmt)
	}
	if stmt.Count {
		count := 0
		for _, row := range table.Rows {
			if matchesWhere(row, stmt.Where) {
				count++
			}
		}
		return Result{Columns: []string{"count"}, Rows: []map[string]any{{"count": count}}}, nil
	}

	var columns []string
	if len(stmt.Columns) == 1 && stmt.Columns[0] == "*" {
		columns = make([]string, 0, len(table.Columns))
		for _, col := range table.Columns {
			columns = append(columns, col.Name)
		}
	} else {
		columns = normalizeColumns(stmt.Columns)
	}

	rows := make([]map[string]any, 0)
	for _, row := range table.Rows {
		if !matchesWhere(row, stmt.Where) {
			continue
		}
		item := make(map[string]any, len(columns))
		for _, col := range columns {
			item[col] = row[col].Any()
		}
		rows = append(rows, item)
	}
	sort.Slice(rows, func(i, j int) bool {
		return fmt.Sprint(rows[i][table.PrimaryKey]) < fmt.Sprint(rows[j][table.PrimaryKey])
	})
	return Result{Columns: columns, Rows: rows}, nil
}

func execUpdate(st *state, stmt UpdateStmt) (Result, error) {
	table, err := requireTable(st, stmt.Table)
	if err != nil {
		return Result{}, err
	}
	if strings.EqualFold(stmt.Where.Column, table.PrimaryKey) {
		row, ok := table.Rows[stmt.Where.Value.String()]
		if !ok {
			return Result{Affected: 0}, nil
		}
		next := row.clone()
		for columnName, raw := range stmt.Assignments {
			column, ok := table.column(columnName)
			if !ok {
				return Result{}, fmt.Errorf("unknown column %s", columnName)
			}
			value, err := coerceValue(raw, column.Type)
			if err != nil {
				return Result{}, err
			}
			next[column.Name] = value
		}
		delete(table.Rows, stmt.Where.Value.String())
		table.Rows[next[table.PrimaryKey].String()] = next
		return Result{Affected: 1}, nil
	}
	affected := 0
	for key, row := range table.Rows {
		if !matchesWhere(row, &stmt.Where) {
			continue
		}
		next := row.clone()
		for columnName, raw := range stmt.Assignments {
			column, ok := table.column(columnName)
			if !ok {
				return Result{}, fmt.Errorf("unknown column %s", columnName)
			}
			value, err := coerceValue(raw, column.Type)
			if err != nil {
				return Result{}, err
			}
			next[column.Name] = value
		}
		delete(table.Rows, key)
		table.Rows[next[table.PrimaryKey].String()] = next
		affected++
	}
	return Result{Affected: affected}, nil
}

func execDelete(st *state, stmt DeleteStmt) (Result, error) {
	table, err := requireTable(st, stmt.Table)
	if err != nil {
		return Result{}, err
	}
	if strings.EqualFold(stmt.Where.Column, table.PrimaryKey) {
		if _, ok := table.Rows[stmt.Where.Value.String()]; !ok {
			return Result{Affected: 0}, nil
		}
		delete(table.Rows, stmt.Where.Value.String())
		return Result{Affected: 1}, nil
	}
	affected := 0
	for key, row := range table.Rows {
		if !matchesWhere(row, &stmt.Where) {
			continue
		}
		delete(table.Rows, key)
		affected++
	}
	return Result{Affected: affected}, nil
}

func requireTable(st *state, name string) (*Table, error) {
	table, ok := st.tables[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("table %s does not exist", name)
	}
	return table, nil
}

func coerceValue(value Value, target ColumnType) (Value, error) {
	switch target {
	case ColumnInt:
		if n, ok := value.Int64(); ok {
			return IntValue(n), nil
		}
		return Value{}, fmt.Errorf("expected int value, got %v", value.Any())
	case ColumnText:
		return TextValue(value.String()), nil
	default:
		return Value{}, fmt.Errorf("unsupported column type %s", target)
	}
}

func zeroValue(target ColumnType) Value {
	if target == ColumnInt {
		return IntValue(0)
	}
	return TextValue("")
}

func matchesWhere(row Row, where *Condition) bool {
	if where == nil {
		return true
	}
	value, ok := row[strings.ToLower(where.Column)]
	if !ok {
		return false
	}
	return value.String() == where.Value.String()
}

func selectByPrimaryKey(table *Table, stmt SelectStmt) (Result, error) {
	row, ok := table.Rows[stmt.Where.Value.String()]
	if stmt.Count {
		if ok {
			return Result{Columns: []string{"count"}, Rows: []map[string]any{{"count": 1}}}, nil
		}
		return Result{Columns: []string{"count"}, Rows: []map[string]any{{"count": 0}}}, nil
	}

	var columns []string
	if len(stmt.Columns) == 1 && stmt.Columns[0] == "*" {
		columns = make([]string, 0, len(table.Columns))
		for _, col := range table.Columns {
			columns = append(columns, col.Name)
		}
	} else {
		columns = normalizeColumns(stmt.Columns)
	}

	if !ok {
		return Result{Columns: columns}, nil
	}

	item := make(map[string]any, len(columns))
	for _, col := range columns {
		item[col] = row[col].Any()
	}
	return Result{Columns: columns, Rows: []map[string]any{item}}, nil
}
