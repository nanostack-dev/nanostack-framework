# Nanostack Framework context

The framework owns reusable service infrastructure shared by Nanostack applications. It leaves product meaning and policies with each application.

## Language

**Framework package:** A narrowly scoped primitive usable independently of the framework's service assembly. Avoid calling an unbounded collection a toolkit or common bucket.

**Framework module:** A reusable lifecycle and dependency-injection integration that makes infrastructure available to an application.

**Service assembly:** The composition of framework modules and application-owned dependencies into one service.

**Application adapter:** Product-owned behavior supplied to a framework integration, such as credential verification or transport error handling.

**Generation configuration:** Explicit inputs and conventions for generating application server, client or database code; generated product contracts remain application-owned.

**Starter:** The proposed baseline for a new service. The current repository contains starter documentation rather than a complete scaffolding command.

**Problem:** Only an RFC 9457 `application/problem+json` object, built and written by `http/problem`. Do not call a config issue or a validation list a problem.

**Field error:** One violation at one location. `validate.Error` is the domain shape (JSON Pointer, code, detail) that generated validators return in a `validate.Errors` list; `problem.FieldError` is the wire shape inside a problem's `errors` member. `http/problem` converts one to the other in one function.

**Issue:** One thing wrong with the configuration, a `config.Issue`: the setting path, its environment variable, where the value came from, the message and the redacted value. One bind reports every issue together in one `*config.Error`.

**Secret:** A sensitive value held as `secret.Secret` from the point it is read. Every printing path shows the redaction marker; `Reveal` is the one call that returns the value.

**Redacted:** The one marker, `[REDACTED]` (`secret.Redacted`), that every redacting path writes in place of a credential: a `Secret`, a credential-named log attribute, a URL password or query parameter, or a credential in a rejected config value.

**Adoption slice:** A bounded consumer migration to a framework capability, validated against the affected application.

Historical vocabulary decisions remain in the [decision log](docs/decision-log.md).
