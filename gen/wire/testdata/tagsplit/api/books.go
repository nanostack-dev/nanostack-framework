package api

import "context"

type booksHandler struct{}

// NewBooksHandler returns its concrete type, which lacks GetBook.
func NewBooksHandler() *booksHandler { return &booksHandler{} }

func (b *booksHandler) ListBooks(context.Context, string) (string, error) { return "", nil }
