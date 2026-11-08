# 0017: Strict encoding/json/v2 decoding at the HTTP boundary

- Status: proposed
- Date: 2026-11-07

## Context

Request bodies are decoded with `encoding/json` (v1). v1 semantics accept input that a strict contract should refuse:

- Duplicate object names are accepted, and the last one wins.
- Invalid UTF-8 is replaced with U+FFFD instead of being rejected.
- Object names match struct fields case-insensitively.

When a validator and the decoder read the same bytes differently, a request can pass validation with one value and bind another. That class of bug caused the oapi-codegen v2.8.1 binding bypass, fixed in the v2.8.2 security release. v1 errors also carry no JSON Pointer, so a client cannot tell which value was wrong.

`encoding/json/v2` and `encoding/json/jsontext` close all three by default, and every decode error carries a JSON Pointer. Measured on Go 1.27.3 on 2026-11-07, with the options below, decoding into ``Name string `json:"name"` ``:

| Body | Result |
| --- | --- |
| `{"name":"a","name":"b"}` | `*jsontext.SyntacticError`, pointer `/name` |
| `{"name":"\xff"}` (invalid UTF-8) | `*jsontext.SyntacticError`, pointer `/name` |
| `{"name":1}` (wrong type) | `*json.SemanticError`, pointer `/name` |
| `{"Name":"a"}` (case differs) | no error; `Name` is not bound, because `"Name"` is an unknown member |

Duplicate names and invalid UTF-8 are syntax errors in v2, not semantic ones. Case-sensitive matching does not reject a case variant; it stops binding it, so the variant is an unknown member like any other. The packages build without `GOEXPERIMENT=jsonv2` on Go 1.27.3.

## Decision

Every request body is decoded with `encoding/json/v2` and strict options, in one framework function that the generated strict server calls.

- The options are set explicitly, even where they are the v2 default, so a future default change or a caller option cannot loosen them: `jsontext.AllowDuplicateNames(false)`, `jsontext.AllowInvalidUTF8(false)` and `json.MatchCaseInsensitiveNames(false)`.
- Both `*jsontext.SyntacticError` and `*json.SemanticError` carry `JSONPointer`, and both become a 400 problem that names it. A duplicate name or invalid UTF-8 therefore names the member, like a wrong type does. A truncated body is also a `SyntacticError`; its pointer is the path read so far, which may be empty for the root.
- Unknown members are not rejected by the decoder: `json.RejectUnknownMembers` is left off, because a schema without `additionalProperties: false` allows them. The generated validation reads the same document and enforces `additionalProperties: false` per object, as a 422 that names each unknown member ([ADR 0016](0016-generated-request-validation.md)).
- A body that decodes runs the generated validation from [ADR 0016](0016-generated-request-validation.md), which answers 422 with every error.
- Handlers never decode request bodies themselves.

## Alternatives

- **v1 with `DisallowUnknownFields`.** Addresses unknown fields only. Duplicate names, invalid UTF-8 and case folding stay, and errors still have no pointer.
- **v1 plus a `jsontext` pre-scan for duplicates.** Two parses of every body, and the validator and binder still use different rules for case and UTF-8.
- **Third-party decoders (goccy/go-json, sonic, jx).** Chosen for speed. They follow v1 semantics or report errors without pointers, and add a dependency the standard library now makes unnecessary.
- **Rely on schema validation to catch it.** A validator that parses its own copy of the body with looser rules cannot see what the binder chose for a duplicate name. The validator and the binder must read the same bytes under the same rules, which strict decoding makes true.

## Consequences

- A client that sends duplicate names or invalid UTF-8 now gets a 400 that names the member. A client that sends a name in another case gets a 422 for that unknown member when the object sets `additionalProperties: false`, and a 422 for the missing field when the field is required. Otherwise the variant is an allowed extra member and the field stays unset, which is what the contract says, since JSON Schema names are case-sensitive too. The validator and the binder agree in every case, because neither folds case. Before rollout, check the clients already released (CLI, MCP server, GitHub Action) and keep the old behaviour server-side until they ship, as the Nanostack workspace `AGENTS.md` ("Cross-repo rollouts") requires.
- Every decode error a client sees carries the JSON Pointer of the value that failed. Only a body cut off between members can point at the root.
- Types with v1 `UnmarshalJSON` methods keep working, since v2 calls them. New custom types implement the v2 `UnmarshalJSONFrom` interface.
- An object schema without `additionalProperties: false` accepts unknown members, including case variants, and the decoder drops them. Contracts that must refuse them set `additionalProperties: false`.
- Response encoding is out of scope. v2 `omitempty` differs from v1 (it omits empty JSON values, not Go zero values), so moving responses to v2 needs its own check against the contract tests.
