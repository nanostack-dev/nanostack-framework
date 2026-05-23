# app

Fluent service composition API built on explicit FX options.

This area should assemble known modules and application options without hiding the underlying service architecture.

Example shape:

```go
app.New("echopoint").
    With(modules.Logging()).
    With(modules.Config()).
    Use(echopointModules...).
    Populate(&server, &db).
    Run()
```
