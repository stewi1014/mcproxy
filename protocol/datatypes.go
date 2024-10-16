package protocol

import (
	"fmt"
	"io"
	"math"
)

type Bool bool

func (a *Bool) ReadBytesFrom(r io.ByteReader) error {
	b, err := r.ReadByte()
	if err == nil {
		*a = b != 0
	}
	return err
}

func (a *Bool) WriteBytesTo(w io.ByteWriter) error {
	b := byte(0)
	if *a {
		b = byte(1)
	}
	return w.WriteByte(b)
}

type Byte int8

func (a *Byte) ReadBytesFrom(r io.ByteReader) error {
	b, err := r.ReadByte()
	if err == nil {
		*a = Byte(b)
	}
	return err
}

func (a *Byte) WriteBytesTo(w io.ByteWriter) error {
	return w.WriteByte(byte(*a))
}

type UByte uint8

func (a *UByte) ReadBytesFrom(r io.ByteReader) error {
	b, err := r.ReadByte()
	if err == nil {
		*a = UByte(b)
	}
	return err
}

func (a *UByte) WriteBytesTo(w io.ByteWriter) error {
	return w.WriteByte(byte(*a))
}

type Short int16

func (a *Short) ReadBytesFrom(r io.ByteReader) error {
	b1, err := r.ReadByte()
	if err != nil {
		return err
	}

	b2, err := r.ReadByte()
	if err != nil {
		return err
	}

	*a = Short(b1)<<8 | Short(b2)
	return nil
}

func (a *Short) WriteBytesTo(w io.ByteWriter) error {
	b1 := byte(*a >> 8)
	b2 := byte(*a)

	err := w.WriteByte(b1)
	if err != nil {
		return err
	}

	err = w.WriteByte(b2)
	if err != nil {
		return err
	}

	return nil
}

type UShort uint16

func (a *UShort) ReadBytesFrom(r io.ByteReader) error {
	b1, err := r.ReadByte()
	if err != nil {
		return err
	}

	b2, err := r.ReadByte()
	if err != nil {
		return err
	}

	*a = UShort(b1)<<8 | UShort(b2)
	return nil
}

func (a *UShort) WriteBytesTo(w io.ByteWriter) error {
	b1 := byte(*a >> 8)
	b2 := byte(*a)

	err := w.WriteByte(b1)
	if err != nil {
		return err
	}

	err = w.WriteByte(b2)
	if err != nil {
		return err
	}

	return nil
}

type Int int32

func (a *Int) ReadBytesFrom(r io.ByteReader) error {
	*a = 0
	for i := range 4 {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}

		*a |= Int(b) << ((3 - i) * 8)
	}
	return nil
}

func (a *Int) WriteBytesTo(w io.ByteWriter) error {
	for i := range 4 {
		b := byte(*a >> ((3 - i) * 8))
		err := w.WriteByte(b)
		if err != nil {
			return err
		}
	}

	return nil
}

type Long int64

func (a *Long) ReadBytesFrom(r io.ByteReader) error {
	*a = 0
	for i := range 8 {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}

		*a |= Long(b) << ((7 - i) * 8)
	}
	return nil
}

func (a *Long) WriteBytesTo(w io.ByteWriter) error {
	for i := range 8 {
		b := byte(*a >> ((7 - i) * 8))
		err := w.WriteByte(b)
		if err != nil {
			return err
		}
	}

	return nil
}

type Float float32

func (a *Float) ReadBytesFrom(r io.ByteReader) error {
	var d uint32
	for i := range 4 {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}

		d |= uint32(b) << (i * 8)
	}
	*a = Float(math.Float32frombits(d))
	return nil
}

func (a *Float) WriteBytesTo(w io.ByteWriter) error {
	d := math.Float32bits(float32(*a))
	for i := range 4 {
		b := byte(d >> (i * 8))
		err := w.WriteByte(b)
		if err != nil {
			return err
		}
	}

	return nil
}

type Double float64

func (a *Double) ReadBytesFrom(r io.ByteReader) error {
	var d uint64
	for i := range 8 {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}

		d |= uint64(b) << (i * 8)
	}
	*a = Double(math.Float64frombits(d))
	return nil
}

func (a *Double) WriteBytesTo(w io.ByteWriter) error {
	d := math.Float64bits(float64(*a))
	for i := range 8 {
		b := byte(d >> (i * 8))
		err := w.WriteByte(b)
		if err != nil {
			return err
		}
	}

	return nil
}

type String255 string

func (a *String255) ReadBytesFrom(r io.ByteReader) error {
	var size VarInt
	err := size.ReadBytesFrom(r)
	if err != nil {
		return err
	}

	if size > 255 {
		return fmt.Errorf(
			"%w: got %v but string is max length 255",
			ErrTooBig,
			size,
		)
	}

	var buff []byte
	for range size {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}

		buff = append(buff, b)
	}

	*a = String255(buff)
	return nil
}

func (a *String255) WriteBytesTo(w io.ByteWriter) error {
	size := VarInt(len(*a))
	err := size.WriteBytesTo(w)
	if err != nil {
		return err
	}

	for _, b := range []byte(*a) {
		err := w.WriteByte(b)
		if err != nil {
			return err
		}
	}

	return nil
}

type String32767 string

func (a *String32767) ReadBytesFrom(r io.ByteReader) error {
	var size VarInt
	err := size.ReadBytesFrom(r)
	if err != nil {
		return err
	}

	if size > 32767 {
		return fmt.Errorf(
			"%w: got %v but string is max length 32767",
			ErrTooBig,
			size,
		)
	}

	var buff []byte
	for range size {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}

		buff = append(buff, b)
	}

	*a = String32767(buff)
	return nil
}

func (a *String32767) WriteBytesTo(w io.ByteWriter) error {
	size := VarInt(len(*a))
	err := size.WriteBytesTo(w)
	if err != nil {
		return err
	}

	for _, b := range []byte(*a) {
		err := w.WriteByte(b)
		if err != nil {
			return err
		}
	}

	return nil
}

type Identifier string
type VarInt int32

func (a *VarInt) ReadBytesFrom(r io.ByteReader) (err error) {
	*a = 0
	b := ^byte(0)
	for i := 0; i < 32 && b&0x80 != 0; i += 7 {
		b, err = r.ReadByte()
		if err != nil {
			return err
		}

		*a |= VarInt(b&0x7F) << i
	}

	return nil
}

func (a *VarInt) WriteBytesTo(w io.ByteWriter) error {
	v := *a
	for {
		if (v &^ 0x7F) == 0 {
			return w.WriteByte(byte(v))
		}

		err := w.WriteByte(byte(v&0x7F) | 0x80)
		if err != nil {
			return err
		}

		v = v >> 7
	}
}

type VarLong int64

func (a *VarLong) ReadBytesFrom(r io.ByteReader) error {
	*a = 0
	b := ^byte(0)
	for i := 0; i < 64 && b&0x80 != 0; i += 7 {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}

		*a |= VarLong(b&0x7F) << i
	}

	return nil
}

func (a *VarLong) WriteBytesTo(w io.ByteWriter) error {
	v := *a
	for {
		if (v &^ 0x7F) == 0 {
			return w.WriteByte(byte(v))
		}

		err := w.WriteByte(byte(v&0x7F) | 0x80)
		if err != nil {
			return err
		}

		v = v >> 7
	}
}

type Position struct {
	X int32
	Z int32
	Y int16
}
type Angle int8
type UUID [16]byte
type BitSet struct {
	Len  int
	Data []byte
}
type FixedBitset struct {
	Len  int
	Data []byte
}
