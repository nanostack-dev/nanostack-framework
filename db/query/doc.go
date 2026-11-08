// Package query runs Nanostack DB statements and maps their results. Absence is not
// failure: a statement that finds nothing returns None or an empty Seq, never
// sql.ErrNoRows.
//
//	stmt := SELECT(Invoice.AllColumns).FROM(Invoice).WHERE(Invoice.ID.EQ(UUID(id)))
//	invoice, err := query.One[model.Invoice](ctx, db, stmt) // (functional.Option[model.Invoice], error)
//	if err != nil {
//		return err
//	}
//	inv, err := invoice.ToResult(ErrInvoiceNotFound).Value() // absence becomes an error here, by choice
//
//	open, err := query.All[model.Invoice](ctx, db, SELECT(...)) // functional.Seq[model.Invoice], never nil
//	n, err := query.Count(ctx, db, SELECT(COUNT(STAR)).FROM(Invoice))
//
// The helpers take the executor explicitly: a *sql.DB, *sql.Tx, *sql.Conn, a stmtcache
// DB or Tx, or anything with QueryContext/ExecContext. They never look a transaction up
// in the context: the context-carried transaction belongs to the caller's transaction
// manager, which passes its executor in, for example
//
//	query.One[model.Invoice](ctx, transactor.Executor(ctx, db), stmt)
//
// Statements run through stmt.QueryContext and stmt.ExecContext, so the statement and
// query loggers (SetLogger, SetQueryLogger) see them. Errors are wrapped once
// ("query.One[model.Invoice]: ..."), so errors.As still finds the driver's error.
// Invalid statements still panic, as they do in jet: they are programming errors.
//
// This package is Nanostack-only (not in go-jet). See db/UPSTREAM.md.
package query
