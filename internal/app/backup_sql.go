package app

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// backupTables are the tables a backup holds: every one exportDatabaseToSQL
// writes, in the order RestoreBackup clears them (children before parents, so
// foreign keys hold). They are also the only tables a restore will write.
var backupTables = []string{
	"share_ring", // FK → shares(id)
	"shares",     // FK → tags(id)
	"follows",    // FK → tags(id)
	"clip_tags",  // FK → clips, tags
	"clips",
	"tags",
	"settings",
	"watched_folders",
	"plugin_storage",
	"plugin_permissions",
	"plugins",
}

// errBackupSQLSyntax marks database.sql content that is not the INSERT-only
// dialect exportDatabaseToSQL writes.
var errBackupSQLSyntax = errors.New("not a mahpastes backup statement")

// backupInsert is one row from database.sql: a table, its columns, and the
// literal values, decoded to the Go types database/sql binds.
type backupInsert struct {
	table   string
	columns []string
	values  []any
}

// backupSQLReader parses database.sql one statement at a time.
//
// RestoreBackup used to split the file on ";\n" and Exec each piece, so a
// backup was a script: whatever it said ran in the restore transaction, on the
// live database — ATTACH a path and create a table there (a file written
// anywhere the user can write), insert an admin row into api_keys (which a
// restore neither clears nor exports), plant a trigger, DROP a table. The only
// statement this app ever writes is
//
//	INSERT INTO <table> (<col>, ...) VALUES (<literal>, ...);
//
// with literals from formatSQLValue: NULL, an integer, a decimal, a '...'
// string with quotes doubled, or an X'...' blob. That is all this reader
// accepts. Values come back decoded, to be bound as parameters, and the
// identifiers are checked against the live schema before any SQL is built
// from them (see backupRowWriter), so nothing in the file is ever executed.
//
// It also reads statement by statement instead of loading the whole file —
// clips are hex-encoded in it, so the file is twice the library's size — and,
// unlike the ";\n" split, keeps a string literal containing ";\n" intact.
type backupSQLReader struct {
	r    *bufio.Reader
	line int
}

func newBackupSQLReader(r io.Reader) *backupSQLReader {
	return &backupSQLReader{r: bufio.NewReaderSize(r, 1<<20), line: 1}
}

func (p *backupSQLReader) errorf(format string, args ...any) error {
	return fmt.Errorf("database.sql line %d: %w: %s", p.line, errBackupSQLSyntax, fmt.Sprintf(format, args...))
}

func (p *backupSQLReader) readByte() (byte, error) {
	b, err := p.r.ReadByte()
	if err == nil && b == '\n' {
		p.line++
	}
	return b, err
}

func (p *backupSQLReader) peekByte() (byte, error) {
	b, err := p.r.Peek(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

// unexpectedEOF turns a mid-statement EOF into a syntax error and passes other
// read errors through.
func (p *backupSQLReader) unexpectedEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return p.errorf("file ends inside a statement")
	}
	return err
}

// skipSpace consumes whitespace and "--" line comments.
func (p *backupSQLReader) skipSpace() error {
	for {
		b, err := p.peekByte()
		if err != nil {
			return err
		}
		switch {
		case b == ' ' || b == '\t' || b == '\r' || b == '\n':
			_, _ = p.readByte()
		case b == '-':
			two, err := p.r.Peek(2)
			if err != nil || two[1] != '-' {
				return nil
			}
			for {
				c, err := p.readByte()
				if err != nil {
					return err
				}
				if c == '\n' {
					break
				}
			}
		default:
			return nil
		}
	}
}

func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentByte(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

// readIdent reads a bare identifier or keyword. Quoted identifiers are not
// part of the dialect.
func (p *backupSQLReader) readIdent() (string, error) {
	if err := p.skipSpace(); err != nil {
		return "", p.unexpectedEOF(err)
	}
	b, err := p.peekByte()
	if err != nil {
		return "", p.unexpectedEOF(err)
	}
	if !isIdentStart(b) {
		return "", p.errorf("expected a name, found %q", b)
	}
	var sb strings.Builder
	for {
		b, err := p.peekByte()
		if err != nil || !isIdentByte(b) {
			break
		}
		_, _ = p.readByte()
		sb.WriteByte(b)
		if sb.Len() > 64 {
			return "", p.errorf("name too long")
		}
	}
	return sb.String(), nil
}

func (p *backupSQLReader) expectKeyword(kw string) error {
	word, err := p.readIdent()
	if err != nil {
		return err
	}
	if !strings.EqualFold(word, kw) {
		return p.errorf("expected %s, found %q", kw, word)
	}
	return nil
}

func (p *backupSQLReader) expectByte(want byte) error {
	if err := p.skipSpace(); err != nil {
		return p.unexpectedEOF(err)
	}
	b, err := p.readByte()
	if err != nil {
		return p.unexpectedEOF(err)
	}
	if b != want {
		return p.errorf("expected %q, found %q", want, b)
	}
	return nil
}

// listNext consumes the separator after a list item: ',' (more follow) or
// ')' (the list is done).
func (p *backupSQLReader) listNext() (more bool, err error) {
	if err := p.skipSpace(); err != nil {
		return false, p.unexpectedEOF(err)
	}
	b, err := p.readByte()
	if err != nil {
		return false, p.unexpectedEOF(err)
	}
	switch b {
	case ',':
		return true, nil
	case ')':
		return false, nil
	}
	return false, p.errorf("expected ',' or ')', found %q", b)
}

// next returns the following INSERT, or io.EOF once only whitespace and
// comments remain. Any other content is a syntax error.
func (p *backupSQLReader) next() (*backupInsert, error) {
	if err := p.skipSpace(); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, err
	}
	if err := p.expectKeyword("INSERT"); err != nil {
		return nil, err
	}
	if err := p.expectKeyword("INTO"); err != nil {
		return nil, err
	}
	table, err := p.readIdent()
	if err != nil {
		return nil, err
	}
	ins := &backupInsert{table: table}

	if err := p.expectByte('('); err != nil {
		return nil, err
	}
	for {
		col, err := p.readIdent()
		if err != nil {
			return nil, err
		}
		ins.columns = append(ins.columns, col)
		more, err := p.listNext()
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
	}

	if err := p.expectKeyword("VALUES"); err != nil {
		return nil, err
	}
	if err := p.expectByte('('); err != nil {
		return nil, err
	}
	for {
		v, err := p.readLiteral()
		if err != nil {
			return nil, err
		}
		ins.values = append(ins.values, v)
		more, err := p.listNext()
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
	}
	if err := p.expectByte(';'); err != nil {
		return nil, err
	}
	return ins, nil
}

// readLiteral reads one value in formatSQLValue's output forms.
func (p *backupSQLReader) readLiteral() (any, error) {
	if err := p.skipSpace(); err != nil {
		return nil, p.unexpectedEOF(err)
	}
	b, err := p.peekByte()
	if err != nil {
		return nil, p.unexpectedEOF(err)
	}
	switch {
	case b == '\'':
		_, _ = p.readByte()
		s, err := p.readQuoted()
		if err != nil {
			return nil, err
		}
		return string(s), nil
	case b == 'X' || b == 'x':
		if two, err := p.r.Peek(2); err == nil && two[1] == '\'' {
			_, _ = p.readByte()
			_, _ = p.readByte()
			return p.readHexBlob()
		}
		return nil, p.errorf("unexpected %q in a value list", b)
	case b == 'N' || b == 'n':
		if err := p.expectKeyword("NULL"); err != nil {
			return nil, err
		}
		return nil, nil
	case b == '-' || (b >= '0' && b <= '9'):
		return p.readNumber()
	}
	return nil, p.errorf("unexpected %q in a value list", b)
}

// readQuoted reads the body of a quoted string, the opening quote already
// consumed, where a doubled quote stands for one. The bytes are kept as they
// are, newlines and all.
func (p *backupSQLReader) readQuoted() ([]byte, error) {
	var buf []byte
	for {
		b, err := p.readByte()
		if err != nil {
			return nil, p.unexpectedEOF(err)
		}
		if b != '\'' {
			buf = append(buf, b)
			continue
		}
		if next, err := p.peekByte(); err == nil && next == '\'' {
			_, _ = p.readByte()
			buf = append(buf, '\'')
			continue
		}
		return buf, nil
	}
}

func hexNibble(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	}
	return 0, false
}

// readHexBlob decodes an X'...' body as it streams, so a large clip is held
// once as bytes, never also as its hex text.
func (p *backupSQLReader) readHexBlob() ([]byte, error) {
	buf := []byte{}
	for {
		hi, err := p.readByte()
		if err != nil {
			return nil, p.unexpectedEOF(err)
		}
		if hi == '\'' {
			return buf, nil
		}
		lo, err := p.readByte()
		if err != nil {
			return nil, p.unexpectedEOF(err)
		}
		h, ok1 := hexNibble(hi)
		l, ok2 := hexNibble(lo)
		if !ok1 || !ok2 {
			return nil, p.errorf("invalid hex in a blob literal")
		}
		buf = append(buf, h<<4|l)
	}
}

// readNumber reads an integer or a decimal (formatSQLValue writes floats with
// %f, so never an exponent).
func (p *backupSQLReader) readNumber() (any, error) {
	var sb strings.Builder
	for {
		b, err := p.peekByte()
		if err != nil || !(b == '-' || b == '.' || (b >= '0' && b <= '9')) {
			break
		}
		_, _ = p.readByte()
		sb.WriteByte(b)
		if sb.Len() > 400 {
			return nil, p.errorf("number too long")
		}
	}
	text := sb.String()
	if strings.Contains(text, ".") {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, p.errorf("invalid number %q", text)
		}
		return f, nil
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return nil, p.errorf("invalid integer %q", text)
	}
	return n, nil
}

// backupRowWriter inserts parsed rows into the restore transaction. The SQL it
// runs is built only from names it found in the live schema — the table from
// backupTables, each column from that table's PRAGMA table_info — and every
// value is a bound parameter.
type backupRowWriter struct {
	tx      *sql.Tx
	columns map[string]map[string]string // table → lower(column) → column
	stmts   map[string]*sql.Stmt
	skipped map[string]int // reason → rows
}

func newBackupRowWriter(tx *sql.Tx) (*backupRowWriter, error) {
	w := &backupRowWriter{
		tx:      tx,
		columns: map[string]map[string]string{},
		stmts:   map[string]*sql.Stmt{},
		skipped: map[string]int{},
	}
	for _, table := range backupTables {
		rows, err := tx.Query(fmt.Sprintf(`SELECT name FROM pragma_table_info('%s')`, table))
		if err != nil {
			return nil, fmt.Errorf("read columns of %s: %w", table, err)
		}
		cols := map[string]string{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return nil, fmt.Errorf("read columns of %s: %w", table, err)
			}
			cols[strings.ToLower(name)] = name
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("read columns of %s: %w", table, err)
		}
		if len(cols) > 0 {
			w.columns[table] = cols
		}
	}
	return w, nil
}

func (w *backupRowWriter) skip(reason string) {
	w.skipped[reason]++
}

// write inserts one row. A row the schema refuses (a constraint) is skipped and
// counted, as is a row for a table a backup does not cover; a column this
// install does not have — a backup from a newer version — is dropped from the
// row, not the row from the table. Any other database error is returned: some
// of them (a full disk) roll the whole transaction back, after which carrying
// on would run every later statement in autocommit on the live library.
func (w *backupRowWriter) write(ins *backupInsert) error {
	table := strings.ToLower(ins.table)
	cols, ok := w.columns[table]
	if !ok {
		w.skip(fmt.Sprintf("rows for table %q, which a backup does not restore", ins.table))
		return nil
	}
	if len(ins.columns) != len(ins.values) {
		w.skip(fmt.Sprintf("%s rows whose column and value counts differ", table))
		return nil
	}
	if reason := refuseBackupRow(table, ins); reason != "" {
		w.skip(reason)
		return nil
	}

	var names []string
	var values []any
	seen := map[string]bool{}
	for i, c := range ins.columns {
		name, ok := cols[strings.ToLower(c)]
		if !ok {
			w.skip(fmt.Sprintf("values of %s.%s, a column this install does not have", table, c))
			continue
		}
		if seen[name] {
			w.skip(fmt.Sprintf("%s rows naming a column twice", table))
			return nil
		}
		seen[name] = true
		names = append(names, `"`+name+`"`)
		values = append(values, ins.values[i])
	}
	if len(names) == 0 {
		w.skip(fmt.Sprintf("%s rows with no column this install has", table))
		return nil
	}
	if table == "clips" {
		names, values = withComputedContentHash(cols, names, values)
	}

	key := table + "\x00" + strings.Join(names, ",")
	stmt := w.stmts[key]
	if stmt == nil {
		placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(names)), ", ")
		var err error
		stmt, err = w.tx.Prepare(fmt.Sprintf(`INSERT INTO "%s" (%s) VALUES (%s)`, table, strings.Join(names, ", "), placeholders))
		if err != nil {
			return fmt.Errorf("prepare %s insert: %w", table, err)
		}
		w.stmts[key] = stmt
	}
	if _, err := stmt.Exec(values...); err != nil {
		if !isRowRefusal(err) {
			return fmt.Errorf("restore %s row: %w", table, err)
		}
		w.skip(fmt.Sprintf("%s rows rejected by the database (%v)", table, err))
	}
	return nil
}

// withComputedContentHash sets a clips row's content_hash from its data. The
// backup's own value is only a claim, and dedup trusts the column: a planted
// hash would make an upload of a well-known file return the backup's clip
// instead, or make DeduplicateAll delete clips whose bytes differ.
func withComputedContentHash(cols map[string]string, names []string, values []any) ([]string, []any) {
	hashCol, ok := cols["content_hash"]
	if !ok {
		return names, values
	}
	var data []byte
	found := false
	for i, n := range names {
		if n == `"`+cols["data"]+`"` {
			switch v := values[i].(type) {
			case []byte:
				data, found = v, true
			case string:
				data, found = []byte(v), true
			}
		}
	}
	if !found {
		return names, values
	}
	hash := computeContentHash(data)
	for i, n := range names {
		if n == `"`+hashCol+`"` {
			values[i] = hash
			return names, values
		}
	}
	return append(names, `"`+hashCol+`"`), append(values, hash)
}

// isRowRefusal reports whether err is SQLite refusing one row — a constraint
// or a datatype mismatch — which rolls back only that statement. Every other
// failure is the database failing, not the row.
func isRowRefusal(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	switch se.Code() & 0xff {
	case sqlite3.SQLITE_CONSTRAINT, sqlite3.SQLITE_MISMATCH:
		return true
	}
	return false
}

// refuseBackupRow returns why a row's values must not be restored, or "".
// The parser only guarantees a row is data; these are the values the app
// would act on as more than data.
func refuseBackupRow(table string, ins *backupInsert) string {
	value := func(col string) (any, bool) {
		if i := indexFold(ins.columns, col); i >= 0 {
			return ins.values[i], true
		}
		return nil, false
	}
	switch table {
	case "clips":
		// The export writes clip data as a blob (or text) literal. A number
		// would bind as INTEGER or REAL, which the BLOB column accepts, and
		// slip past withComputedContentHash with whatever hash the row claims.
		if v, ok := value("data"); ok {
			switch v.(type) {
			case []byte, string:
			default:
				return "clip rows whose data is not bytes"
			}
		}
	case "settings":
		// The export never writes sensitive keys (see exportDatabaseToSQL), so
		// one here did not come from this app; and a key that is not text
		// would slip a sensitive name past that check.
		if v, ok := value("key"); ok {
			key, isText := v.(string)
			if !isText {
				return "settings rows whose key is not text"
			}
			if isSensitiveSetting(key) {
				return "sensitive settings, which backups never carry"
			}
		}
	case "plugin_permissions":
		// Listed on the plugin card as stored; only the kinds of grant the
		// plugin APIs make are real permissions.
		if v, ok := value("permission_type"); ok {
			kind, isText := v.(string)
			if !isText || !restorablePermissionTypes[kind] {
				return "plugin permission rows of a type the app never grants"
			}
		}
	case "plugins":
		// The plugin manager joins filename to its plugins dir to read the
		// plugin and, on removal, to delete it: a path here reads or deletes
		// any file the user can.
		if v, ok := value("filename"); ok {
			name, isText := v.(string)
			if !isText || !isBarePluginFilename(name) {
				return "plugin rows whose filename is not a bare .lua file name"
			}
		}
	}
	return ""
}

// restorablePermissionTypes are the plugin_permissions.permission_type values
// the app writes: filesystem grants (plugin/api_fs.go) and url-setting network
// grants (plugin.Manager.SetStorageWithGrant).
var restorablePermissionTypes = map[string]bool{"fs_read": true, "fs_write": true, "network": true}

// isBarePluginFilename reports whether name is a plain file name in the
// plugins dir: no directory part on any platform, and a .lua extension.
func isBarePluginFilename(name string) bool {
	return name != "" &&
		!strings.ContainsAny(name, "/\\\x00") &&
		filepath.Base(name) == name &&
		strings.HasSuffix(name, ".lua") && name != ".lua"
}

// close releases the prepared statements and reports what was skipped.
func (w *backupRowWriter) close() {
	for _, stmt := range w.stmts {
		_ = stmt.Close()
	}
	for reason, n := range w.skipped {
		fmt.Printf("Warning: restore skipped %d %s\n", n, reason)
	}
}

func indexFold(list []string, want string) int {
	for i, s := range list {
		if strings.EqualFold(s, want) {
			return i
		}
	}
	return -1
}

// restoreBackupRows replays database.sql into tx through backupSQLReader and
// backupRowWriter. A statement outside the INSERT-only dialect fails the whole
// restore: that file was not written by this app, and the transaction is
// rolled back with nothing applied.
func restoreBackupRows(tx *sql.Tx, src io.Reader) error {
	w, err := newBackupRowWriter(tx)
	if err != nil {
		return err
	}
	defer w.close()

	reader := newBackupSQLReader(src)
	for {
		ins, err := reader.next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := w.write(ins); err != nil {
			return err
		}
	}
}

// countBackupRows reads database.sql the way restoreBackupRows does and counts
// the rows a restore would write to the tables the confirm dialog reports. The
// manifest's own summary is only what the backup claims: one saying "0
// plugins" restored and ran whatever plugins rows the SQL held. Rows the
// writer would skip before reaching the database are not counted; a row the
// schema itself refuses at insert time still is, which can only overstate.
func countBackupRows(src io.Reader) (BackupSummary, error) {
	var sum BackupSummary
	reader := newBackupSQLReader(src)
	for {
		ins, err := reader.next()
		if errors.Is(err, io.EOF) {
			return sum, nil
		}
		if err != nil {
			return sum, err
		}
		table := strings.ToLower(ins.table)
		if len(ins.columns) != len(ins.values) || refuseBackupRow(table, ins) != "" {
			continue
		}
		switch table {
		case "clips":
			sum.Clips++
		case "tags":
			sum.Tags++
		case "plugins":
			sum.Plugins++
		case "watched_folders":
			sum.WatchFolders++
		}
	}
}
