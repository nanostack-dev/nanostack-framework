# transactor

Context-carried SQL transaction helper for code paths that need transaction propagation across several services or repositories.

Prefer explicit transaction parameters where they keep repository boundaries clearer. Use this package when the application already relies on context propagation for transactional work.
