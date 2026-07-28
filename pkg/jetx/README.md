# jetx

Go-Jet helpers that sit *above* query execution: ordering, expression
conversion, and filter composition.

Query execution lives in `pkg/db/transactor`, which is the single entry point
for running statements. It carries transactions through context and returns a
`Result[T]`, so a constraint violation can be translated where the statement is
issued.

This package used to export a parallel set of query helpers taking an explicit
`*DBOptions{Tx}`. They were removed: nothing used them, and offering two ways to
run a query meant one of them silently bypassed `Result`'s constraint
translation.
