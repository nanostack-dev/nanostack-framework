# jetx

Go-Jet query helpers and explicit transaction options.

`jetx.DBOptions` matches the explicit transaction-passing style used by existing repositories. Context-carried transactions live in `pkg/db/transactor`; applications can choose either pattern, but repositories should make transaction boundaries obvious.
