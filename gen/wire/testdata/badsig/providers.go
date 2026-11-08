package badsig

type Thing struct{}

func (*Thing) Method() *Thing { return nil }

func NewTwoValues() (*Thing, string) { return nil, "" }
func NewVariadic(...string) *Thing   { return nil }
func NewNothing()                    {}

type Store interface{ Get() string }

func Returns() int { return 0 }
