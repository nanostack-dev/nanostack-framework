// Package repository stores billing records.
package repository

type Repository struct{}

func New() *Repository { return &Repository{} }
