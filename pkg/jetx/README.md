# jetx

Go-Jet helpers that sit *above* query execution: ordering, expression
conversion, and filter composition.

Every `Build*` helper returns `nil` when it has nothing to filter on, and
`CombineFilters` drops those nils. A repository can therefore build every filter
unconditionally and let the unset search fields fall away:

```go
where := jetx.CombineFilters(
    table.Workspaces.ProductID.EQ(jet.String(productID)),
    jetx.BuildIDFilter(table.Workspaces.ID, input.Filter.IDs),
    jetx.BuildSubstringFilter([]jet.ColumnString{table.Workspaces.Name}, input.Filter.Search),
)
```

`BuildSubstringFilter` is a plain `LIKE '%term%'`. Use a tsvector column and a
real full-text query when you need ranking or stemming.

Query execution lives in `pkg/db/transactor`, which is the single entry point
for running statements. It carries transactions through context and returns a
`Result[T]`, so a constraint violation can be translated where the statement is
issued.

This package used to export a parallel set of query helpers taking an explicit
`*DBOptions{Tx}`. They were removed: nothing used them, and offering two ways to
run a query meant one of them silently bypassed `Result`'s constraint
translation.
