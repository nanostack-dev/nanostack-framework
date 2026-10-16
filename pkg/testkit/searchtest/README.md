# searchtest

Checks a cursor-paginated search through its HTTP API, from a consumer's component tests. It covers the contract every search shares; the suite owns the fixtures and the endpoint.

```go
searchtest.Run(t, searchtest.Suite{
    Name: "searchFlows",
    Call: func(ctx context.Context, body map[string]any) (int, []byte, error) { /* POST body, return status and body */ },
    Cases: []searchtest.Case{
        {Name: "default", Body: map[string]any{}, Want: idsInDefaultOrder},
        {Name: "name desc", Body: map[string]any{"sort_by": "name", "sort_direction": "DESC"}, Want: idsByNameDesc},
    },
    Refusals: searchtest.Refusals{
        Scopes: []searchtest.Scope{
            {Body: map[string]any{}, Incompatible: map[string]any{"sort_by": "name"}},
            {Body: map[string]any{"sort_by": "name"}},
            {Body: map[string]any{"filter": map[string]any{"ids": []string{"none"}}}, Incompatible: map[string]any{"sort_by": "name"}},
        },
        SortField: true,
    },
    Mutate: searchtest.FrameworkCursorMutations,
})
```

- **Walks.** Each case walks by cursor at `Limit` (default 2) and must return exactly `Want`, in order: pages within the limit, `count == len(items)`, a non-negative `total` that is the same on every page and equal to the rows walked, a `next_cursor` that always advances, no empty page after the first, and no `next_cursor` once every expected row has arrived. `Want` comes from the fixture, not from the search, so an ignored or mis-mapped sort fails. Seed more rows than the limit, with ties on the sort column, and wait for any asynchronous projection before running.
- **Refusals.** In every scope the kit first sends the scope as-is (it must answer 200), then a malformed cursor, an oversized cursor, a cursor with an offset, and, when `SortField` is set, an unknown `sort_by`; each must answer 400 whose error `code` fields include `INVALID_CURSOR` or `INVALID_SORT` (override with `CursorCode` and `SortCode`). Include scopes where the search could return early (no match, empty parent, missing binding): validation must still run there. A scope's `Incompatible` is a populated request with a different ordering; the cursor it issues must be refused in that scope, empty or not.
- **Forged cursors.** `Mutate` returns variants of a real cursor; each must answer 200 with a valid page or 400, never a 5xx. `FrameworkCursorMutations` forges cursors issued by this framework's keyset ordering (adversarial values for every key, wrong arity, another ordering, another version, broken encodings). Leave it unset for a search that forwards another service's cursor: that service owns its format.
- **Decoding.** `DecodeItems("items", "id")` is the default and rejects a body missing `items`, `total`, `count` or an item id. Pass a custom `Decoder` for another shape, such as a grouped response, and run one suite per group.

`CheckWalk` and `CheckRefusals` return errors instead of failing a test, for callers that drive the checks themselves. See `searchtest_integration_test.go` for a complete suite against a real Postgres search.
