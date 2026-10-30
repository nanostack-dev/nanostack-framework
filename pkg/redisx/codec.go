package redisx

import (
	"encoding/json"
	"fmt"
)

// Codec turns a T into the string Redis stores, and back.
type Codec[T any] interface {
	Encode(value T) (string, error)
	Decode(raw string) (T, error)
}

// JSON encodes values with encoding/json. It is the default codec for every
// type except string.
func JSON[T any]() Codec[T] { return jsonCodec[T]{} }

// Text stores strings as they are. It is the default codec for string.
func Text() Codec[string] { return textCodec{} }

type jsonCodec[T any] struct{}

func (jsonCodec[T]) Encode(value T) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("redisx: encode %T: %w", value, err)
	}
	return string(data), nil
}

func (jsonCodec[T]) Decode(raw string) (T, error) {
	var value T
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return value, fmt.Errorf("redisx: decode %T: %w", value, err)
	}
	return value, nil
}

type textCodec struct{}

func (textCodec) Encode(value string) (string, error) { return value, nil }
func (textCodec) Decode(raw string) (string, error)   { return raw, nil }

func defaultCodec[T any]() Codec[T] {
	var zero T
	if _, ok := any(zero).(string); ok {
		codec, _ := any(textCodec{}).(Codec[T])
		return codec
	}
	return jsonCodec[T]{}
}

func encodeAll[T any](codec Codec[T], values []T) ([]any, error) {
	encoded := make([]any, len(values))
	for i, value := range values {
		raw, err := codec.Encode(value)
		if err != nil {
			return nil, err
		}
		encoded[i] = raw
	}
	return encoded, nil
}

func decodeAll[T any](codec Codec[T], raws []string) ([]T, error) {
	values := make([]T, len(raws))
	for i, raw := range raws {
		value, err := codec.Decode(raw)
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	return values, nil
}
