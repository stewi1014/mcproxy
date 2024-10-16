package protocol

import (
	"encoding/json"
	"io"
	"reflect"
)

type Packet interface {
	ID() VarInt
	Encodable
}

type Encodable interface {
	ReadBytesFrom(io.ByteReader) error
	WriteBytesTo(io.ByteWriter) error
}

func decodeFields(r io.ByteReader, schema any) error {
	v := reflect.ValueOf(schema).Elem()

	for i := range v.NumField() {
		f := v.Field(i).Addr()
		p := f.Interface().(Encodable)
		err := p.ReadBytesFrom(r)
		if err != nil {
			return err
		}
	}

	return nil
}

func encodeFields(w io.ByteWriter, schema any) error {
	v := reflect.ValueOf(schema).Elem()

	for i := range v.NumField() {
		f := v.Field(i).Addr()
		p := f.Interface().(Encodable)
		err := p.WriteBytesTo(w)
		if err != nil {
			return err
		}
	}

	return nil
}

var _ Packet = &HandshakeIntention{}

type HandshakeIntention struct {
	ProtocolVersion VarInt
	ServerAddress   String255
	ServerPort      UShort
	NextState       VarInt
}

func (h *HandshakeIntention) ReadBytesFrom(r io.ByteReader) error {
	return decodeFields(r, h)
}

func (h *HandshakeIntention) WriteBytesTo(w io.ByteWriter) error {
	return encodeFields(w, h)
}

func (h *HandshakeIntention) ID() VarInt {
	return 0
}

type StatusRequest struct{}

func (h *StatusRequest) ReadBytesFrom(r io.ByteReader) error {
	return decodeFields(r, h)
}

func (h *StatusRequest) WriteBytesTo(w io.ByteWriter) error {
	return encodeFields(w, h)
}

func (h *StatusRequest) ID() VarInt {
	return 0
}

type StatusResponse struct {
	JSONResponse struct {
		Version struct {
			Name     string `json:"name"`
			Protocol int    `json:"protocol"`
		} `json:"version"`
		Players struct {
			Max    int `json:"max"`
			Online int `json:"online"`
			Sample []struct {
				Name string
				ID   string
			} `json:"sample,omitempty"`
		} `json:"players"`
		Description       string `json:"description"`
		Favicon           string `json:"favicon,omitempty"`
		EnforceSecureChat bool   `json:"enforcesSecureChat"`
	}
}

func (h *StatusResponse) ReadBytesFrom(r io.ByteReader) error {
	var str String32767
	err := str.ReadBytesFrom(r)
	if err != nil {
		return err
	}

	return json.Unmarshal([]byte(str), &h.JSONResponse)
}

func (h *StatusResponse) WriteBytesTo(w io.ByteWriter) error {
	data, err := json.Marshal(h.JSONResponse)
	if err != nil {
		return err
	}

	str := String32767(data)
	return str.WriteBytesTo(w)
}

func (h *StatusResponse) ID() VarInt {
	return 0
}

type PingRequest struct {
	Payload Long
}

func (h *PingRequest) ReadBytesFrom(r io.ByteReader) error {
	return decodeFields(r, h)
}

func (h *PingRequest) WriteBytesTo(w io.ByteWriter) error {
	return encodeFields(w, h)
}

func (h *PingRequest) ID() VarInt {
	return 1
}

type PongResponse struct {
	Payload Long
}

func (h *PongResponse) ReadBytesFrom(r io.ByteReader) error {
	return decodeFields(r, h)
}

func (h *PongResponse) WriteBytesTo(w io.ByteWriter) error {
	return encodeFields(w, h)
}

func (h *PongResponse) ID() VarInt {
	return 1
}

type Disconnect struct {
	JSONTextComponent struct {
		Text string `json:"text"`
	}
}

func (h *Disconnect) ReadBytesFrom(r io.ByteReader) error {
	var str String32767
	err := str.ReadBytesFrom(r)
	if err != nil {
		return err
	}

	return json.Unmarshal([]byte(str), &h.JSONTextComponent)
}

func (h *Disconnect) WriteBytesTo(w io.ByteWriter) error {
	data, err := json.Marshal(h.JSONTextComponent)
	if err != nil {
		return err
	}

	str := String32767(data)
	return str.WriteBytesTo(w)
}

func (h *Disconnect) ID() VarInt {
	return 0
}
