// Package fault is the runtime behind generated error catalogues.
//
// Each module declares its errors in a Go file and `nano gen errors` writes a
// typed error per declaration. Every generated error implements Described, the
// contract the RFC 9457 problem writer renders, plus Coded and Detailed.
//
// The package is named fault, not errs, for two reasons. The generated
// catalogue packages are what call sites read (billingerr.InvoiceNotFound(id)),
// and services often name them errs, so a runtime called errs would force an
// import alias wherever both meet. And errs is the usual name of a local error
// accumulator (var errs []error), which would shadow the package. fault also
// continues the vocabulary of pkg/fault, which stays the error type of
// existing services until they migrate to http/problem.
//
// This package is the single owner of the problem contract (Described, Coded,
// Detailed) and of the problem type URI base. http/problem imports these
// interfaces and renders them; it declares no copy.
package fault

// Described is an error that carries its RFC 9457 problem identity. The problem
// writer maps any error that implements it to application/problem+json.
type Described interface {
	error
	ProblemType() string
	ProblemStatus() int
	ProblemTitle() string
}

// IsErrorStatus reports whether status is an HTTP error status, 400 to 599.
func IsErrorStatus(status int) bool { return status >= 400 && status <= 599 }

// Valid reports whether d describes a problem of its own: an HTTP error
// status and a non-empty type. It is the one rule for a usable Described. The
// problem writer renders any other Described as a 500, and core/log then
// counts it as carrying no status, so the level and the response agree.
func Valid(d Described) bool {
	return IsErrorStatus(d.ProblemStatus()) && d.ProblemType() != ""
}

// Coded is implemented by errors with a stable machine code, for example
// billing.invoice_already_paid. The problem writer adds it as the "code"
// extension member.
type Coded interface {
	Described
	ProblemCode() string
}

// Detailed is implemented by errors with an occurrence-specific explanation,
// the RFC 9457 "detail" member. An empty detail means none.
type Detailed interface {
	Described
	ProblemDetail() string
}

// DefaultTypeBase is the problem type URI base used when a catalogue does not
// set its own. It has no trailing slash: a type URI is
// <base>/<namespace>/<code>.
const DefaultTypeBase = "https://errors.nanostack.dev"

// Kind identifies one catalogue entry. Generated catalogues declare one Kind
// per error as the sentinel for errors.Is:
//
//	if errors.Is(err, billingerr.ErrInvoiceNotFound) { ... }
//
// A Kind is itself a Described error, so returning the sentinel directly still
// renders the right problem (without fields or detail).
type Kind struct {
	code    string
	status  int
	title   string
	typeURI string
}

// NewKind declares a catalogue entry. Generated code calls it; the generator
// has already validated the code, status and URI.
func NewKind(code string, status int, title, typeURI string) *Kind {
	return &Kind{code: code, status: status, title: title, typeURI: typeURI}
}

// Error returns "<code>: <title>".
func (k *Kind) Error() string { return k.code + ": " + k.title }

// ProblemType returns the problem type URI.
func (k *Kind) ProblemType() string { return k.typeURI }

// ProblemStatus returns the HTTP status.
func (k *Kind) ProblemStatus() int { return k.status }

// ProblemTitle returns the short, occurrence-independent summary.
func (k *Kind) ProblemTitle() string { return k.title }

// ProblemCode returns the stable code, <namespace>.<name>.
func (k *Kind) ProblemCode() string { return k.code }

var (
	_ Coded = (*Kind)(nil)
)
