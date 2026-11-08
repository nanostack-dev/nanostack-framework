package functional

import (
	"bytes"
	"encoding/json"
)

var jsonNull = []byte("null")

// MarshalJSON writes null for an absent Option and the value's JSON otherwise.
// A present zero value is written as that value (Some(0) is 0, not null); use the
// omitzero tag option to leave an absent Option out of an object.
func (o Option[T]) MarshalJSON() ([]byte, error) {
	if !o.present {
		return []byte("null"), nil
	}
	// &o.v (o is a copy) lets a T whose MarshalJSON has a pointer receiver encode
	// the same way it does as an addressable struct field.
	return json.Marshal(&o.v)
}

// UnmarshalJSON reads null as None and anything else as Some of the decoded T.
// The inner value is always decoded with encoding/json.
//
// On error, the receiver is left unchanged.
func (o *Option[T]) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), jsonNull) {
		*o = None[T]()
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*o = Some(v)
	return nil
}
