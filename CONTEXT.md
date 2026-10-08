# Nanostack Framework context

The framework owns reusable service infrastructure shared by Nanostack applications. It leaves product meaning and policies with each application.

## Language

**Framework package:** A narrowly scoped primitive usable independently of the framework's service assembly. Avoid calling an unbounded collection a toolkit or common bucket.

**Framework module:** A reusable lifecycle and dependency-injection integration that makes infrastructure available to an application.

**Service assembly:** The composition of framework modules and application-owned dependencies into one service.

**Application adapter:** Product-owned behavior supplied to a framework integration, such as credential verification or transport error handling.

**Generation configuration:** Explicit inputs and conventions for generating application server, client or database code; generated product contracts remain application-owned.

**Starter:** The proposed baseline for a new service. The current repository contains starter documentation rather than a complete scaffolding command.

**Adoption slice:** A bounded consumer migration to a framework capability, validated against the affected application.

Historical vocabulary decisions remain in the [decision log](docs/decision-log.md).
