package api

import "context"

type authorsHandler struct{}

// NewAuthorsHandler returns its tag interface, which authorsHandler does
// not implement: the package does not compile.
func NewAuthorsHandler() AuthorsServer { return &authorsHandler{} }

type booksHandler struct{}

// NewBooksHandler has the right methods.
func NewBooksHandler() BooksServer { return &booksHandler{} }

func (b *booksHandler) ListBooks(context.Context, string) (string, error) { return "", nil }
func (b *booksHandler) GetBook(context.Context, string) (string, error)   { return "", nil }

// unrelated is another compile error of the package; wire leaves it to go build.
var unrelated int = "not an int"
