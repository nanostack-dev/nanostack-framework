# 0022: Error catalogues are declared as a Go interface, with explicit codes

- Status: accepted
- Date: 2026-11-07

## Context

The framework plan (§14) makes error codes data: one `errors.yaml` per service generates Go constructors, the problem type URIs, an OpenAPI `components/responses` fragment and documentation. Its example builds an error with string keys: `errs.InvoiceAlreadyPaid.With("id", inv.ID).With("paid_at", inv.PaidAt)`.

`nano gen errors` was built against a different source: a Go interface annotated `//nano:errors`, one method per error. The method's parameters become typed fields of the generated error. The first version derived the stable code and the type URI from the Go method name (`InvoiceNotFound` became `invoice_not_found`), so renaming a method for readability changed the client contract without any warning except the `ERRORS.md` diff.

## Decision

1. **The source is a Go interface, not YAML.** Each method declares one error. Its parameters are typed (`id ids.UUID, paidAt time.Time`) and the generated constructor has the same signature, so a call site with a wrong or missing field does not compile. The generator type-checks the package with `go/packages`, so parameter types come from any package under any import name, and it type-checks its output before returning it.
2. **Every error writes its code.** `//nano:error code=invoice_not_found status=404 title="..."`. `code=` is required. The code and the type URI `<type_base>/<namespace>/<code>` never follow the Go name, so a rename changes call sites only. The generator suggests the snake-case code when it is missing.
3. **The OpenAPI responses fragment is deferred** until a generator reads the problem schema that `http/problem` owns. The `Kinds` list gives that generator every entry in declaration order. The roadmap tracks it.

## Alternatives

- **`errors.yaml`, as planned.** Data that agents edit without Go knowledge. But fields are untyped (`fields: [id, paid_at]`), so a missing or mistyped field is a runtime surprise, and the YAML needs its own type vocabulary to say that `paid_at` is a time. The Go interface gets types, editor navigation and refactoring for free, and agents edit it as easily as YAML.
- **Codes derived from the method name, guarded by a codes lock file that `--check` compares.** It keeps declarations shorter, but it adds a second committed file that only restates the codes, and the snake-case mapping produces odd permanent codes (`IPv6Invalid` became `i_pv6_invalid`). Writing the code once, next to the error, is simpler and makes the contract visible where it is declared.
- **Codes derived from the name, with the `ERRORS.md` diff as the only guard.** Rejected: a reviewer must notice that a Go rename changed a public string.

## Consequences

- Declarations carry `code=` on every error. Changing a code is a breaking API change, visible in the declaration and in `ERRORS.md`.
- Codes are unique within a namespace. The generator works on one package, so it cannot check namespaces across packages; each service keeps its namespaces distinct.
- The plan's `With(...)` builder does not exist. Errors are built with typed constructors: `billingerr.InvoiceAlreadyPaid(inv.ID, inv.PaidAt).Wrap(err)`.
- The plan document (§14 and the decisions table) still describes `errors.yaml`; it should point to this record.
- Related configuration decisions from the same change: `nano gen config` also writes `.env.example`, and `config.Profile` / `config.ProfileFromEnv` layer `base.yaml` and `<NANO_PROFILE>.yaml`, as plan §13 describes.
