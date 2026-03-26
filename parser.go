package nekosql

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Statement interface{ isStatement() }

type ColumnType string

const (
	ColumnInt  ColumnType = "INT"
	ColumnText ColumnType = "TEXT"
)

type ColumnDef struct {
	Name       string
	Type       ColumnType
	PrimaryKey bool
}

type Value struct {
	raw any
}

func IntValue(v int64) Value   { return Value{raw: v} }
func TextValue(v string) Value { return Value{raw: v} }

func (v Value) Any() any       { return v.raw }
func (v Value) String() string { return fmt.Sprint(v.raw) }
func (v Value) Int64() (int64, bool) {
	switch x := v.raw.(type) {
	case int64:
		return x, true
	case int:
		return int64(x), true
	case float64:
		return int64(x), true
	case string:
		n, err := strconv.ParseInt(x, 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

type Condition struct {
	Column string
	Value  Value
}

type CreateTableStmt struct {
	Name    string
	Columns []ColumnDef
}

type InsertStmt struct {
	Table   string
	Columns []string
	Values  []Value
}

type SelectStmt struct {
	Table   string
	Columns []string
	Where   *Condition
	Count   bool
}

type UpdateStmt struct {
	Table       string
	Assignments map[string]Value
	Where       Condition
}

type DeleteStmt struct {
	Table string
	Where Condition
}

func (CreateTableStmt) isStatement() {}
func (InsertStmt) isStatement()      {}
func (SelectStmt) isStatement()      {}
func (UpdateStmt) isStatement()      {}
func (DeleteStmt) isStatement()      {}

func Parse(sql string) (Statement, error) {
	sql = strings.TrimSpace(strings.TrimSuffix(sql, ";"))
	switch {
	case hasPrefixFold(sql, "CREATE TABLE "):
		return parseCreateTable(sql)
	case hasPrefixFold(sql, "INSERT INTO "):
		return parseInsert(sql)
	case hasPrefixFold(sql, "SELECT "):
		return parseSelect(sql)
	case hasPrefixFold(sql, "UPDATE "):
		return parseUpdate(sql)
	case hasPrefixFold(sql, "DELETE FROM "):
		return parseDelete(sql)
	default:
		return nil, fmt.Errorf("unsupported sql: %s", sql)
	}
}

func parseCreateTable(sql string) (Statement, error) {
	body := strings.TrimSpace(sql[len("CREATE TABLE "):])
	open := strings.Index(body, "(")
	close := strings.LastIndex(body, ")")
	if open <= 0 || close <= open {
		return nil, errors.New("invalid CREATE TABLE syntax")
	}

	name := strings.TrimSpace(body[:open])
	rawColumns := splitCSV(body[open+1 : close])
	columns := make([]ColumnDef, 0, len(rawColumns))
	for _, raw := range rawColumns {
		parts := strings.Fields(raw)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid column definition %q", raw)
		}
		column := ColumnDef{Name: parts[0]}
		switch strings.ToUpper(parts[1]) {
		case "INT":
			column.Type = ColumnInt
		case "TEXT":
			column.Type = ColumnText
		default:
			return nil, fmt.Errorf("unsupported column type %s", parts[1])
		}
		if len(parts) > 2 && strings.EqualFold(strings.Join(parts[2:], " "), "PRIMARY KEY") {
			column.PrimaryKey = true
		}
		columns = append(columns, column)
	}
	return CreateTableStmt{Name: name, Columns: columns}, nil
}

func parseInsert(sql string) (Statement, error) {
	body := strings.TrimSpace(sql[len("INSERT INTO "):])
	valuesIdx := indexFold(body, " VALUES ")
	if valuesIdx < 0 {
		return nil, errors.New("invalid INSERT syntax")
	}

	head := strings.TrimSpace(body[:valuesIdx])
	valuePart := strings.TrimSpace(body[valuesIdx+len(" VALUES "):])
	open := strings.Index(head, "(")
	close := strings.LastIndex(head, ")")
	if open <= 0 || close <= open {
		return nil, errors.New("invalid INSERT column list")
	}

	table := strings.TrimSpace(head[:open])
	columns := normalizeColumns(splitCSV(head[open+1 : close]))

	vOpen := strings.Index(valuePart, "(")
	vClose := strings.LastIndex(valuePart, ")")
	if vOpen != 0 || vClose <= vOpen {
		return nil, errors.New("invalid INSERT values")
	}

	rawValues := splitCSV(valuePart[vOpen+1 : vClose])
	values := make([]Value, 0, len(rawValues))
	for _, raw := range rawValues {
		value, err := parseValue(raw)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return InsertStmt{Table: table, Columns: columns, Values: values}, nil
}

func parseSelect(sql string) (Statement, error) {
	body := strings.TrimSpace(sql[len("SELECT "):])
	fromIdx := indexFold(body, " FROM ")
	if fromIdx < 0 {
		return nil, errors.New("invalid SELECT syntax")
	}

	columnExpr := strings.TrimSpace(body[:fromIdx])
	rest := strings.TrimSpace(body[fromIdx+len(" FROM "):])
	whereIdx := indexFold(rest, " WHERE ")

	table := rest
	var where *Condition
	if whereIdx >= 0 {
		table = strings.TrimSpace(rest[:whereIdx])
		cond, err := parseCondition(rest[whereIdx+len(" WHERE "):])
		if err != nil {
			return nil, err
		}
		where = &cond
	}

	if strings.EqualFold(columnExpr, "COUNT(*)") {
		return SelectStmt{Table: table, Count: true, Where: where}, nil
	}
	return SelectStmt{Table: table, Columns: splitCSV(columnExpr), Where: where}, nil
}

func parseUpdate(sql string) (Statement, error) {
	body := strings.TrimSpace(sql[len("UPDATE "):])
	setIdx := indexFold(body, " SET ")
	whereIdx := indexFold(body, " WHERE ")
	if setIdx < 0 || whereIdx < 0 || whereIdx <= setIdx {
		return nil, errors.New("invalid UPDATE syntax")
	}

	assignments := make(map[string]Value)
	for _, raw := range splitCSV(body[setIdx+len(" SET ") : whereIdx]) {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid assignment %q", raw)
		}
		value, err := parseValue(parts[1])
		if err != nil {
			return nil, err
		}
		assignments[strings.ToLower(strings.TrimSpace(parts[0]))] = value
	}

	where, err := parseCondition(body[whereIdx+len(" WHERE "):])
	if err != nil {
		return nil, err
	}
	return UpdateStmt{Table: strings.TrimSpace(body[:setIdx]), Assignments: assignments, Where: where}, nil
}

func parseDelete(sql string) (Statement, error) {
	body := strings.TrimSpace(sql[len("DELETE FROM "):])
	whereIdx := indexFold(body, " WHERE ")
	if whereIdx < 0 {
		return nil, errors.New("DELETE requires WHERE")
	}
	where, err := parseCondition(body[whereIdx+len(" WHERE "):])
	if err != nil {
		return nil, err
	}
	return DeleteStmt{Table: strings.TrimSpace(body[:whereIdx]), Where: where}, nil
}

func parseCondition(raw string) (Condition, error) {
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) != 2 {
		return Condition{}, errors.New("only equality WHERE is supported")
	}
	value, err := parseValue(parts[1])
	if err != nil {
		return Condition{}, err
	}
	return Condition{
		Column: strings.ToLower(strings.TrimSpace(parts[0])),
		Value:  value,
	}, nil
}

func parseValue(raw string) (Value, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return TextValue(strings.ReplaceAll(raw[1:len(raw)-1], "''", "'")), nil
	}
	number, err := strconv.ParseInt(raw, 10, 64)
	if err == nil {
		return IntValue(number), nil
	}
	return TextValue(raw), nil
}

func splitCSV(raw string) []string {
	parts := make([]string, 0)
	start := 0
	inString := false
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '\'':
			inString = !inString
		case ',':
			if !inString {
				parts = append(parts, strings.TrimSpace(raw[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(raw[start:]))
	return parts
}

func normalizeColumns(columns []string) []string {
	out := make([]string, 0, len(columns))
	for _, col := range columns {
		out = append(out, strings.ToLower(strings.TrimSpace(col)))
	}
	return out
}

func hasPrefixFold(s, prefix string) bool {
	return strings.HasPrefix(strings.ToUpper(s), strings.ToUpper(prefix))
}
func indexFold(s, needle string) int {
	return strings.Index(strings.ToUpper(s), strings.ToUpper(needle))
}
