package msgpack

import (
	"bytes"
	"io"

	"github.com/pkg/errors"
	"github.com/vmihailenco/msgpack/v5"
)

type Namer interface {
	Name() string
}

type Converter struct {
	buf     *bytes.Buffer
	encoder *msgpack.Encoder
}

func New() Converter {
	var buf bytes.Buffer
	enc := msgpack.NewEncoder(&buf)
	enc.UseCompactFloats(true)
	enc.UseCompactInts(true)
	enc.UseArrayEncodedStructs(true)
	enc.UseInternedStrings(true)
	enc.SetOmitEmpty(true)
	return Converter{
		buf:     &buf,
		encoder: enc,
	}
}

func (c Converter) Marshal(v interface{}) ([]byte, error) {
	if err := c.encoder.Encode(v); err != nil {
		return nil, err
	}
	return io.ReadAll(c.buf)
}

func (c Converter) MarshalEnum(n Namer) ([]byte, error) {
	if err := c.encoder.Encode(map[string]interface{}{n.Name(): n}); err != nil {
		return nil, err
	}
	return io.ReadAll(c.buf)
}

func (c Converter) Unmarshal(data []byte, v interface{}) error {
	return msgpack.Unmarshal(data, v)
}

func Unmarshal(data []byte, v interface{}) error {
	return msgpack.Unmarshal(data, v)
}

func UnmarshalEnum[T Namer](data []byte, n *T) error {
	m := make(map[string]T, 1)
	if err := msgpack.Unmarshal(data, &m); err != nil {
		return err
	}
	if res, exists := m[(*n).Name()]; exists {
		*n = res
		return nil
	}
	return errors.Errorf("doesn't exists feild=%s", (*n).Name())
}

func CovertTo[T byte | int | int16 | int64 | int32 | uint32, V byte | int | int16 | int64 | int32 | uint32](data []T) []V {
	res := make([]V, len(data))
	for i := range data {
		res[i] = V(data[i])
	}
	return res
}
