package transactor

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	dbsearch "github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
)

// The four functions below run the searches of db/searchexec in the context
// transaction. Repository code runs a search through them, never through
// searchexec.Run or Plan.Run with an executor of its own: those run on the
// executor they get, do not read the context transaction and do not refuse a
// row lock.
//
// They take and return the types of db/search, not of pkg/search, because a
// generated search is built on db/search.

// Search runs the generated search q with Executor(ctx, db): it counts the
// matches and reads one page in order, mapping each row with toDomain. spec is
// the search's generated spec variable. It is searchexec.Run in the context
// transaction, so it refuses what searchexec.Run refuses before any statement
// runs, a query built from another spec included (search.ErrUnscoped).
//
// A search page never locks rows: under a requested row lock (ForShare,
// ForUpdate, WithRowLock and the others), Search returns ErrRowLockSearch and
// runs nothing.
//
// Search returns a Result, so the On* translations compose on it before Value.
func Search[T any, Q searchexec.Builder[T, Q, S, P], S ~string, P comparable, R any](
	ctx context.Context,
	db qrm.DB,
	spec *searchexec.Spec[T, S, P],
	q searchexec.Query[T, Q, S, P],
	toDomain func(T) R,
) Result[dbsearch.Result[R]] {
	if rowLockRequested(ctx) {
		return newResult(dbsearch.Result[R]{}, ErrRowLockSearch)
	}
	result, err := searchexec.Run(ctx, Executor(ctx, db), spec, q, toDomain)
	return newResult(result, err)
}

// SearchPage is Search with a mapper that maps the whole page in one call, for
// example to load the children of the page's rows in one statement. It is
// searchexec.RunPage in the context transaction, with the row-lock refusal of
// Search.
func SearchPage[T any, Q searchexec.Builder[T, Q, S, P], S ~string, P comparable, R any](
	ctx context.Context,
	db qrm.DB,
	spec *searchexec.Spec[T, S, P],
	q searchexec.Query[T, Q, S, P],
	toDomain func(context.Context, []T) ([]R, error),
) Result[dbsearch.Result[R]] {
	if rowLockRequested(ctx) {
		return newResult(dbsearch.Result[R]{}, ErrRowLockSearch)
	}
	result, err := searchexec.RunPage(ctx, Executor(ctx, db), spec, q, toDomain)
	return newResult(result, err)
}

// RunPlan runs a plan that a hand-written repository method got from
// searchexec.Resolve, in order (from plan.Order), mapping each row with
// toDomain. It is plan.Run in the context transaction, with the row-lock
// refusal of Search. The zero Plan is search.ErrUnscoped.
//
//	plan, err := searchexec.Resolve(ctx, webhookSearchSpec, q.Query)
//	if err != nil {
//		return search.Result[webhook.Webhook]{}, err
//	}
//	order, err := plan.Order(map[webhook.SortField]keyset.Key[webhookRow]{
//		webhook.SortFieldRequestCount: requestCountKey,
//	})
//	if err != nil {
//		return search.Result[webhook.Webhook]{}, err
//	}
//	return transactor.RunPlan(ctx, r.db, plan, order, r.toDomain).Value()
func RunPlan[T any, S ~string, R any](
	ctx context.Context,
	db qrm.DB,
	plan searchexec.Plan[T, S],
	order keyset.Order[T],
	toDomain func(T) R,
) Result[dbsearch.Result[R]] {
	if rowLockRequested(ctx) {
		return newResult(dbsearch.Result[R]{}, ErrRowLockSearch)
	}
	result, err := plan.Run(ctx, Executor(ctx, db), order, toDomain)
	return newResult(result, err)
}

// RunPlanPage is RunPlan with a mapper that maps the whole page in one call. It
// is plan.RunPage in the context transaction, with the row-lock refusal of
// Search.
func RunPlanPage[T any, S ~string, R any](
	ctx context.Context,
	db qrm.DB,
	plan searchexec.Plan[T, S],
	order keyset.Order[T],
	toDomain func(context.Context, []T) ([]R, error),
) Result[dbsearch.Result[R]] {
	if rowLockRequested(ctx) {
		return newResult(dbsearch.Result[R]{}, ErrRowLockSearch)
	}
	result, err := plan.RunPage(ctx, Executor(ctx, db), order, toDomain)
	return newResult(result, err)
}
