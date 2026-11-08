package functional

import "database/sql"

// Scan implements database/sql.Scanner. NULL becomes None. A value becomes Some.
//
// A src that already has type T is stored as it is. That is how Nanostack DB's row
// mapper, which converts values itself, hands Scan a finished value. A []byte src never
// takes this path, because the driver owns that memory: it is copied first.
// Every other src goes through sql.Null[T], so it follows the same conversion rules
// database/sql applies when it scans into a plain *T (rows.Scan(&opt) behaves like
// rows.Scan(&ptr)).
//
// Option has no driver.Valuer: its Value() T method already takes that name. To pass an
// Option to raw database/sql as an argument, use ToPtr(); database/sql turns a nil
// pointer into NULL. Nanostack DB statements unwrap Options themselves.
//
// On error, the receiver is left unchanged.
func (o *Option[T]) Scan(src any) error {
	if src == nil {
		*o = None[T]()
		return nil
	}
	if _, isBytes := src.([]byte); !isBytes {
		if v, ok := src.(T); ok {
			*o = Some(v)
			return nil
		}
	}
	var n sql.Null[T]
	if err := n.Scan(src); err != nil {
		return err
	}
	*o = Some(n.V)
	return nil
}
