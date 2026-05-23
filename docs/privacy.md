# Privacy And Publication

The framework should be published from a clean repository history unless a different strategy is explicitly chosen.

## Current Position

- Do not publish old `nanostack-shared` history as the initial framework history.
- Keep `nanostack-shared` available locally as migration source material and compatibility bridge.
- Move code into `nanostack-framework` through new commits authored under the intended Nanostack identity.
- Avoid linking personal identity in public-facing docs, repository metadata, or initial framework history.

## Remote Strategy

The first remote should be private.

Before pushing, verify:

- the local framework repository has no unwanted personal identity in tracked files
- git author metadata for new framework commits uses the intended Nanostack identity
- no secrets or local machine configuration files are tracked
- old shared history has not been imported unless explicitly approved

## Compatibility Strategy

Compatibility shims may remain in `nanostack-shared` while `anchor` and `echopoint` migrate. The compatibility repository does not need to become the public face of the framework.
