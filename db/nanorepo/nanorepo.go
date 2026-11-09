// Package nanorepo declares repositories for nano gen repo. Its functions do
// nothing at run time: a declaration is a call inside func _() { ... }, which
// Go never runs, and nano gen repo reads it from source. The package is part
// of Nanostack DB, not of go-jet, and imports only the standard library.
//
//	func _() {
//		nanorepo.Implement[ProductUserRepository, productUserRepositoryImpl](
//			table.ProductUsers, productUserToDomain)
//	}
//
// For every table a declaration names, nano gen repo writes a base into
// repo_base.gen.go in the declaring package: productUsersBase, with the
// table (b.t), typed predicates (b.where.Email(v)), helpers that run through
// pkg/db/transactor (findOne, first, findAll, exists, count, deleteWhere,
// updateWhere) and the transactor functions as methods (query, queryRow,
// exec, page, ...). The implementation embeds the base and sets it in its
// constructor:
//
//	type productUserRepositoryImpl struct {
//		productUsersBase
//	}
//
//	func NewProductUserRepository(db *sql.DB) ProductUserRepository {
//		return &productUserRepositoryImpl{productUsersBase: newProductUsersBase(db)}
//	}
//
// The declaring file carries no build tag, so go build, go vet and gopls
// type-check the declaration and a rename updates it.
package nanorepo

// Table is satisfied by every table type that nano gen db generates.
type Table interface {
	TableName() string
}

// Implement binds the repository interface I to its implementation Impl, a
// struct type of the declaring package, on table. toDomain maps one row of
// the generated model E to the domain type D that the base's row helpers
// return; E and D are inferred from it. toDomain is a function, a variable
// of the declaring package set to a function or to a method value on a type
// without fields, or NoMapper: the base calls it on its own, so a mapper
// that needs state is mapped in hand-written methods instead.
// Two declarations on one table in one package share the base and pass the
// same toDomain.
//
//nolint:revive // the parameters document the call; nano gen repo reads the arguments from source
func Implement[I, Impl, E, D any](table Table, toDomain func(E) D) {}

// NoMapper is the toDomain of a table whose rows the base returns as the
// generated model itself:
//
//	nanorepo.Implement[Repository, repositoryImpl](table.Products, nanorepo.NoMapper[model.Products])
func NoMapper[E any](row E) E { return row }

// Base declares only the base of table, with rows of the generated model E
// and no mapper, for a table that a repository reads or writes without an
// interface bound to it.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func Base[E any](table Table) {}
