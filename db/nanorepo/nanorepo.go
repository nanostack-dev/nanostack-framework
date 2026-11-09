// Package nanorepo declares repositories for nanostack gen repo. Its functions do
// nothing at run time: a declaration is a call inside func _() { ... }, which
// Go never runs, and nanostack gen repo reads it from source. The package is part
// of Nanostack DB, not of go-jet, and imports only the standard library.
//
//	func _() {
//		nanorepo.Implement[ProductUserRepository, productUserRepositoryImpl](
//			table.ProductUsers, productUserToDomain)
//	}
//
// For every table a declaration names, nanostack gen repo writes a base into
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
// Each method of the interface that the implementation does not have is
// derived from its name, such as FindByTenantIDAndSku, into
// <file>_repo.gen.go next to the declaring file, or nanostack gen repo fails at
// the method with the signature to write by hand.
//
// The declaring file carries no build tag, so go build, go vet and gopls
// type-check the declaration and a rename updates it.
package nanorepo

// Table is satisfied by every table type that nanostack gen db generates.
type Table interface {
	TableName() string
}

// Implement binds the repository interface I to its implementation Impl, a
// struct type of the declaring package, on table. Every method of I is a
// method of Impl or derived from its name on table. toDomain maps one row of
// the generated model E to the domain type D that the base's row helpers
// return; E and D are inferred from it. toDomain is a function, a variable
// of the declaring package set to a function or to a method value on a type
// without fields, or NoMapper: the base calls it on its own, so a mapper
// that needs state is mapped in hand-written methods instead.
// Two declarations on one table in one package share the base and pass the
// same toDomain.
//
// The options are ToRow, which lets the writes of I derive, and OnUnique,
// which maps a unique violation of a derived write to an error of the
// service.
//
//nolint:revive // the parameters document the call; nanostack gen repo reads the arguments from source
func Implement[I, Impl, E, D any](table Table, toDomain func(E) D, options ...Option) {}

// Option is an option of Implement: ToRow or OnUnique. A search option is
// not one.
type Option interface {
	implementOption()
}

type implementOption struct{}

func (implementOption) implementOption() {}

// ToRow is the other direction of toDomain: it maps a domain value to the row
// of the generated model that a derived write (Create, CreateIn..., UpdateBy...,
// UpsertBy...) inserts or updates. E and D must be the row and domain types of
// toDomain; nanostack gen repo checks them. toRow follows the rule of toDomain: a
// function, a variable of the declaring package set to a function or to a
// method value on a type without fields. A declaration with NoMapper needs
// no ToRow: its writes take the model itself.
//
//	nanorepo.Implement[ProductPermissionRepository, productPermissionRepositoryImpl](
//		table.ProductPermissions, productPermissionToDomain,
//		nanorepo.ToRow(productPermissionToRow))
//
//nolint:revive // the parameter documents the call; nanostack gen repo reads the argument from source
func ToRow[E, D any](toRow func(D) E) Option { return implementOption{} }

// OnUnique makes every derived write of the declaration return target when
// the statement violates one of the named unique constraints or unique
// indexes: transactor's Result.OnUnique on each of them. target is a
// package-level error variable; each name is a string constant that
// nanostack gen repo finds among the unique keys and indexes the table file
// lists. A violation of another constraint keeps its PostgreSQL error.
//
//	nanorepo.OnUnique(ErrProductPermissionExists,
//		"product_permissions_pkey", "idx_product_permissions_product_lower_name_unique")
//
//nolint:revive // the parameters document the call; nanostack gen repo reads the arguments from source
func OnUnique(target error, constraint string, more ...string) Option { return implementOption{} }

// NoMapper is the toDomain of a table whose rows the base returns as the
// generated model itself:
//
//	nanorepo.Implement[Repository, repositoryImpl](table.Products, nanorepo.NoMapper[model.Products])
func NoMapper[E any](row E) E { return row }

// Base declares only the base of table, with rows of the generated model E
// and no mapper, for a table that a repository reads or writes without an
// interface bound to it.
//
//nolint:revive // the parameter documents the call; nanostack gen repo reads the argument from source
func Base[E any](table Table) {}
