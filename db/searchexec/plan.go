package searchexec

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/query"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
)

// errKeyConflict reports a hand-written key that has the name of a declared sort.
var errKeyConflict = errors.New("searchexec: a hand-written key has the name of a declared sort")

// Plan is a search resolved for one run: the source, the full condition (scope, context
// scopes, fixed conditions and filters), the sorts with the default applied, and the checked
// page. Resolve returns it to a hand-written repository method.
//
// A Plan is opaque. It has no exported condition, so a hand-written method can narrow the
// search with And but never widen it. The zero Plan does not run: its methods return
// search.ErrUnscoped.
type Plan[T any, S ~string] struct {
	source    keyset.Source
	columns   jet.Projection
	condition jet.BoolExpression
	keys      map[S]keyset.Key[T]
	tiebreak  []keyset.Key[T]
	sorts     []search.Sort[S]
	page      search.Pagination
}

// And returns the plan with extra added to its condition with AND, so it keeps only rows
// that the plan keeps already. A nil extra adds nothing.
func (p Plan[T, S]) And(extra jet.BoolExpression) Plan[T, S] {
	if extra == nil || p.condition == nil {
		return p
	}
	p.condition = jet.AND(p.condition, extra)
	return p
}

// Order returns the keyset order of the plan's sorts. extra adds the keys of hand-written
// sorts (a computed key); a key of extra with the name of a declared sort is an error. A
// sort that is in neither the declared keys nor extra is search.ErrUnknownSort.
func (p Plan[T, S]) Order(extra map[S]keyset.Key[T]) (keyset.Order[T], error) {
	if p.condition == nil {
		return keyset.Order[T]{}, errUnresolved
	}
	keys := p.keys
	if len(extra) > 0 {
		keys = maps.Clone(p.keys)
		for name, key := range extra {
			if _, ok := keys[name]; ok {
				return keyset.Order[T]{}, fmt.Errorf("%w: %q", errKeyConflict, name)
			}
			keys[name] = key
		}
	}
	return keyset.OrderOf(p.sorts, keys, p.tiebreak[0], p.tiebreak[1:]...)
}

// Sorts returns the sorts of the plan, with the default applied.
func (p Plan[T, S]) Sorts() []search.Sort[S] {
	return slices.Clone(p.sorts)
}

// Page returns the checked page of the plan.
func (p Plan[T, S]) Page() search.Pagination {
	return p.page
}

// Run counts the matches and reads one page in order, mapping each row with toDomain. db is
// the executor of both statements.
//
// It runs on db as given: it does not read the context transaction and does not refuse a
// row lock. Repository code runs searches through the transactor forwarders
// (transactor.Search, SearchPage, RunPlan and RunPlanPage, in a later release), which use
// the context transaction and refuse a row lock.
func (p Plan[T, S]) Run[R any](
	ctx context.Context, db query.Executor, order keyset.Order[T], toDomain func(T) R,
) (search.Result[R], error) {
	if p.condition == nil {
		return search.Result[R]{}, errUnresolved
	}
	return keyset.Page(db, toDomain, p.columns).
		From(p.source).
		Where(p.condition).
		RequireWhere().
		Keyset(order).
		Run(ctx, p.page)
}

// RunPage is Run with a mapper that maps the whole page in one call, for example to load
// the children of the page's rows in one statement. A nil result is an empty page.
//
// It runs on db as given: it does not read the context transaction and does not refuse a
// row lock. Repository code runs searches through the transactor forwarders
// (transactor.Search, SearchPage, RunPlan and RunPlanPage, in a later release), which use
// the context transaction and refuse a row lock.
func (p Plan[T, S]) RunPage[R any](
	ctx context.Context, db query.Executor, order keyset.Order[T], toDomain func(context.Context, []T) ([]R, error),
) (search.Result[R], error) {
	if p.condition == nil {
		return search.Result[R]{}, errUnresolved
	}
	return keyset.PageOf(db, toDomain, p.columns).
		From(p.source).
		Where(p.condition).
		RequireWhere().
		Keyset(order).
		Run(ctx, p.page)
}

// errUnresolved reports a Plan that Resolve did not return.
var errUnresolved = fmt.Errorf("%w: the plan was not returned by Resolve", search.ErrUnscoped)

// Resolve checks q and resolves it for one run. spec is the search's generated spec
// variable; a hand-written repository method passes it, as the generated method does.
//
// Resolve refuses, in this order:
//   - a nil spec, a query built from another spec, and a struct literal of the builder
//     (search.ErrUnscoped);
//   - a zero scope value (search.ErrUnscoped, naming the columns), and a zero field of the
//     scope struct that no ScopeCol reads, for example a field that only From reads
//     (search.ErrUnscoped, naming the field);
//   - a context scope that returns nil (search.ErrUnscoped);
//   - the first error that a filter recorded: more than search.MaxInValues values
//     (search.ErrTooManyValues), a term longer than search.MaxTermLength characters
//     (search.ErrTermTooLong), a time bound outside the years that every dialect holds
//     (search.ErrInvalidTime), or an Exists child that is not a table (keyset.ErrDialect);
//   - a sort direction other than ASC or DESC (search.ErrInvalidSortDirection);
//   - a page that search.CheckPage refuses (search.ErrInvalidLimit, search.ErrInvalidOffset,
//     search.ErrInvalidCursor, search.ErrCursorWithOffset);
//   - a source that is not a table or join of db/postgres, db/mysql or db/sqlite, and an
//     Exists child of another dialect than the source (keyset.ErrDialect).
//
// No statement runs before these checks pass.
func Resolve[T any, Q Builder[T, Q, S, P], S ~string, P comparable](
	ctx context.Context, spec *Spec[T, S, P], q Query[T, Q, S, P],
) (Plan[T, S], error) {
	if spec == nil {
		return Plan[T, S]{}, fmt.Errorf("%w: no spec", search.ErrUnscoped)
	}
	if q.spec != spec {
		return Plan[T, S]{}, fmt.Errorf("%w: the query was not built from this search's spec", search.ErrUnscoped)
	}
	conditions := make([]jet.BoolExpression, 0, len(spec.scope)+len(spec.context)+len(spec.conditions)+len(q.where))
	for _, scope := range spec.scope {
		condition, err := scope.condition(q.scope)
		if err != nil {
			return Plan[T, S]{}, err
		}
		if condition != nil {
			conditions = append(conditions, condition)
		}
	}
	if field := zeroScopeField(q.scope); field != "" {
		return Plan[T, S]{}, fmt.Errorf("%w: the scope field %s is zero", search.ErrUnscoped, field)
	}
	for i, scope := range spec.context {
		condition := scope(ctx)
		if condition == nil {
			return Plan[T, S]{}, fmt.Errorf("%w: context scope %d returned no condition", search.ErrUnscoped, i)
		}
		conditions = append(conditions, condition)
	}
	if q.err != nil {
		return Plan[T, S]{}, q.err
	}
	for _, sort := range q.sorts {
		if sort.Direction != search.SortAscending && sort.Direction != search.SortDescending {
			return Plan[T, S]{}, fmt.Errorf("%w: %q", search.ErrInvalidSortDirection, sort.Direction)
		}
	}
	if err := search.CheckPage(q.page); err != nil {
		return Plan[T, S]{}, err
	}
	source, err := searchSource(spec.from(q.scope))
	if err != nil {
		return Plan[T, S]{}, err
	}
	if err := checkChildren(source, q.children); err != nil {
		return Plan[T, S]{}, err
	}
	conditions = append(conditions, spec.conditions...)
	conditions = append(conditions, q.where...)
	sorts := slices.Clone(q.sorts)
	if len(sorts) == 0 && spec.sort.Field != "" {
		sorts = []search.Sort[S]{spec.sort}
	}
	// A search scoped only through From (ScopeFrom) has no WHERE condition. DefineSpec
	// already proved its scope, so the plan runs with TRUE.
	condition := sqlTrue()
	if len(conditions) > 0 {
		condition = jet.AND(conditions...)
	}
	return Plan[T, S]{
		source:    source,
		columns:   spec.columns,
		condition: condition,
		keys:      spec.keys,
		tiebreak:  spec.tiebreak,
		sorts:     sorts,
		page:      q.page,
	}, nil
}

// Run resolves q, counts the matches and reads one page in order, mapping each row with
// toDomain. It refuses what Resolve refuses, and a sort that is not a key of the spec
// (search.ErrUnknownSort). db is the executor of both statements.
//
// It runs on db as given: it does not read the context transaction and does not refuse a
// row lock. Repository code runs searches through the transactor forwarders
// (transactor.Search, SearchPage, RunPlan and RunPlanPage, in a later release), which use
// the context transaction and refuse a row lock.
func Run[T any, Q Builder[T, Q, S, P], S ~string, P comparable, R any](
	ctx context.Context, db query.Executor, spec *Spec[T, S, P], q Query[T, Q, S, P], toDomain func(T) R,
) (search.Result[R], error) {
	plan, err := Resolve(ctx, spec, q)
	if err != nil {
		return search.Result[R]{}, err
	}
	order, err := plan.Order(nil)
	if err != nil {
		return search.Result[R]{}, err
	}
	return plan.Run(ctx, db, order, toDomain)
}

// RunPage is Run with a mapper that maps the whole page in one call.
//
// It runs on db as given: it does not read the context transaction and does not refuse a
// row lock. Repository code runs searches through the transactor forwarders
// (transactor.Search, SearchPage, RunPlan and RunPlanPage, in a later release), which use
// the context transaction and refuse a row lock.
func RunPage[T any, Q Builder[T, Q, S, P], S ~string, P comparable, R any](
	ctx context.Context,
	db query.Executor,
	spec *Spec[T, S, P],
	q Query[T, Q, S, P],
	toDomain func(context.Context, []T) ([]R, error),
) (search.Result[R], error) {
	plan, err := Resolve(ctx, spec, q)
	if err != nil {
		return search.Result[R]{}, err
	}
	order, err := plan.Order(nil)
	if err != nil {
		return search.Result[R]{}, err
	}
	return plan.RunPage(ctx, db, order, toDomain)
}

// searchSource checks the source of a search: a table or join of db/postgres (which also
// serves CockroachDB), db/mysql (which also serves MariaDB) or db/sqlite. Any other source is
// keyset.ErrDialect, before any statement runs.
func searchSource(source keyset.Source) (keyset.Source, error) {
	switch source.(type) {
	case nil:
		return nil, keyset.ErrPageSourceMissing
	case postgres.ReadableTable, mysql.ReadableTable, sqlite.ReadableTable:
		return source, nil
	default:
		return nil, fmt.Errorf("%w: %T is not a table or join of db/postgres, db/mysql or db/sqlite",
			keyset.ErrDialect, source)
	}
}

// checkChildren refuses an Exists child of another dialect than source. A nested SELECT is
// written in the dialect of the outer statement, so such a child would name a table of
// another database.
func checkChildren(source keyset.Source, children []jet.Dialect) error {
	var dialect jet.Dialect
	switch source.(type) {
	case postgres.ReadableTable:
		dialect = postgres.Dialect
	case mysql.ReadableTable:
		dialect = mysql.Dialect
	case sqlite.ReadableTable:
		dialect = sqlite.Dialect
	}
	for _, child := range children {
		if child != dialect {
			return fmt.Errorf("%w: an Exists child is a table of %s, the search is on %s",
				keyset.ErrDialect, child.Name(), dialect.Name())
		}
	}
	return nil
}
