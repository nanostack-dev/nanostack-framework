# workflow

Workflow helpers belong in `pkg/workflow` first, not `modules/workflow`.

Reasoning:

- feature packages should keep final workflow names and environment suffixing close to their workflow definitions
- helper functions for publish, activate, and worker lifecycle can be reusable without hiding feature ownership
- an FX module can be added later only if multiple services need the same lifecycle wrapper

Applications still own workflow definitions, step functions, worker IDs, and naming policy.
