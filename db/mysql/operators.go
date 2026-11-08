// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package mysql

import "github.com/nanostack-dev/nanostack-framework/db/internal/jet"

// NOT returns negation of bool expression result
var NOT = jet.NOT

// BIT_NOT inverts every bit in integer expression result
var BIT_NOT = jet.BIT_NOT

// DISTINCT operator can be used to return distinct values of expr
var DISTINCT = jet.DISTINCT
