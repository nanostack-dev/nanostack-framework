// Package samepkg generates into the manifest package (-out .), so
// unexported providers are allowed.
package samepkg

import "context"

type greeter struct{ greeting string }

func newGreeter() *greeter { return &greeter{greeting: "hi"} }

type server struct{ g *greeter }

func newServer(g *greeter) *server            { return &server{g: g} }
func (*server) Run(ctx context.Context) error { <-ctx.Done(); return nil }
