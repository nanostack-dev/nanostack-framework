// Package repository stores invoices. Its name and its type's name are the
// billing repository's too.
package repository

type Repository struct{}

func New() *Repository { return &Repository{} }
