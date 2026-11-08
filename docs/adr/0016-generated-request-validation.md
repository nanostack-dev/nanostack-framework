# 0016: Generated request validation replaces kin-openapi at runtime

- Status: proposed
- Date: 2026-11-07

## Context

Services are contract-first: oapi-codegen generates types and a strict server interface from each OpenAPI document. Request validation happens at runtime in three places:

- `modules/httpserver` mounts the kin-openapi `openapi3filter` middleware, which loads the document at startup and walks the schema for every request.
- `pkg/apisec` builds a kin-openapi router (the `gorillamux` router) from the document to find each request's security requirements.
- `pkg/validate` wraps go-playground/validator, a second rule system driven by struct tags and reflection.

kin-openapi interprets the document at runtime, so a schema keyword it does not support is skipped without an error, and the request is validated against a document that is parsed separately from the struct the handler receives.

An ogen trial on echopoint's OpenAPI 3.1 document (181 operations, 379 schemas) showed that generated validators work: all 18 must-reject inputs were rejected. ogen itself was rejected for a fatal parse error on a multi-type schema, crashes in its domain-type mapping on Go 1.27, and decode errors that stop at the first error and carry no JSON Pointer.

## Decision

The framework generates request validation from the OpenAPI document with `nano gen validate`.

- Every request schema gets a generated `Validate(validate.Path) validate.Errors` method that covers `required`, `minimum`/`maximum`, `minLength`/`maxLength`, `pattern`, `format`, `enum`, array bounds, `oneOf`/`anyOf` with discriminators, and `additionalProperties: false`. The decoder drops unknown members ([ADR 0017](0017-strict-json-v2-decoding.md)), so the generated code checks `additionalProperties` against the request document, the same bytes the decoder read. Errors accumulate, and each carries the JSON Pointer of the failing value.
- The generated strict server calls it through oapi-codegen's user-template override, which is a supported option, not a fork. A request that fails validation gets one 422 `application/problem+json` response that lists every error.
- A schema keyword the generator does not support is a generation error, never a silent skip.
- Security requirements per operation are generated too, so `pkg/apisec` stops loading the document and routing it at runtime.
- go-playground/validator is removed. Configuration validation is generated with the configuration binder, and domain invariants use `functional.Validation[T]` constructors.
- kin-openapi stays as a test dependency. `pkg/testkit/openapi` and contract tests use it as an independent oracle: responses are checked against the document, and a differential test checks that the generated validators reject what kin-openapi rejects.

## Alternatives

- **Keep the kin-openapi middleware.** Rejected. It interprets the document per request, skips what it does not understand, validates a separately parsed document instead of the bound struct, and produces error shapes the framework has to translate.
- **ogen.** Generated validation done right, but rejected on the trial's quality findings above.
- **oapi-codegen's request validator middleware.** It is kin-openapi underneath, so it has the same runtime model.
- **Validator struct tags emitted by oapi-codegen (`x-oapi-codegen-extra-tags`).** Keeps reflection and the second rule system, and tags cannot express `oneOf` or discriminators.
- **Code-first frameworks (huma, goa).** They derive or own the document instead of reading it, which contradicts contract-first generation.

## Consequences

- The framework owns the validator generator and its golden tests, and must track which JSON Schema keywords it supports.
- A schema change that the generator cannot express fails `nano gen` instead of passing unvalidated.
- Request validation no longer loads or walks the document at startup or per request. The embedded document is still served.
- Products lose struct-tag validation and move those rules to the schema, generated configuration validation or `Validation[T]` constructors.
- Strict body decoding, which runs before validation, is a separate decision: [ADR 0017](0017-strict-json-v2-decoding.md).
