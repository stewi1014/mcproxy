package protocol

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
)

const (
	StateHandshaking   = 0
	StateStatus        = 1
	StateLogin         = 2
	StateConfiguration = 3
	StatePlay          = 4
)

var (
	ErrUnknownPacketId = errors.New("unknown packet id")
	ErrTooBig          = errors.New("size too big")
)

type closerFunc func() error

func (c closerFunc) Close() error {
	return c()
}

func NewConn(rwc io.ReadWriteCloser) *Conn {
	return &Conn{
		rwc:         rwc,
		r:           bufio.NewReader(rwc),
		w:           bufio.NewWriter(rwc),
		state:       0,
		compression: -1,
	}
}

type Conn struct {
	rwc io.ReadWriteCloser
	r   *bufio.Reader
	w   *bufio.Writer

	state       VarInt
	version     VarInt
	compression int32
}

func (c *Conn) PipeTo(ctx context.Context, handshake *HandshakeIntention, conn net.Conn) error {
	s := NewConn(conn)
	defer s.rwc.Close()
	defer c.rwc.Close()

	err := s.WritePacket(handshake)
	if err != nil {
		return err
	}

	err = s.w.Flush()
	if err != nil {
		return err
	}

	_, err = io.CopyN(s.rwc, c.r, int64(c.r.Buffered()))
	if err != nil {
		return err
	}

	err = c.w.Flush()
	if err != nil {
		return err
	}

	_, err = io.CopyN(c.rwc, s.r, int64(s.r.Buffered()))
	if err != nil {
		return err
	}

	ctx, done := context.WithCancelCause(ctx)
	go func() {
		_, err := io.Copy(c.rwc, s.rwc)
		done(err)
	}()

	go func() {
		_, err := io.Copy(s.rwc, c.rwc)
		done(err)
	}()

	<-ctx.Done()
	return context.Cause(ctx)
}

func (c *Conn) Version() int {
	return int(c.version)
}

func (c *Conn) ReadPacket(listenFor ...Packet) (Packet, error) {
	_, packetId, reader, err := c.readPacket()
	if err != nil {
		return nil, err
	}

	for _, packet := range listenFor {
		if packet.ID() == packetId {
			err := packet.ReadBytesFrom(reader)
			if err != nil {
				return packet, err
			}

			if handshake, ok := packet.(*HandshakeIntention); ok {
				c.state = handshake.NextState
				c.version = handshake.ProtocolVersion
			}

			return packet, nil
		}
	}

	return nil, fmt.Errorf("%w %v", ErrUnknownPacketId, packetId)
}

func (c *Conn) readPacket() (size, packetId VarInt, packetReader io.ByteReader, err error) {
	if c.compression < 0 {
		err = size.ReadBytesFrom(c.r)
		if err != nil {
			return
		}

		if size < 0 {
			err = errors.New("negative packet size")
			return
		}

		packetReader = bufio.NewReader(io.LimitReader(c.r, int64(size)))
		err = packetId.ReadBytesFrom(packetReader)
		return
	}

	panic("not implemented")
}

func (c *Conn) WritePacket(packet Packet) error {
	writer, closer := c.writePacket(packet.ID())
	err := packet.WriteBytesTo(writer)
	if err != nil {
		return err
	}

	return closer.Close()
}

func (c *Conn) writePacket(packetId VarInt) (io.ByteWriter, io.Closer) {
	buff := new(bytes.Buffer)

	closer := func() error {
		var packetIdBuff bytes.Buffer
		err := packetId.WriteBytesTo(&packetIdBuff)
		if err != nil {
			return err
		}

		size := VarInt(packetIdBuff.Len() + buff.Len())
		err = size.WriteBytesTo(c.w)
		if err != nil {
			return err
		}

		_, err = packetIdBuff.WriteTo(c.w)
		if err != nil {
			return err
		}

		_, err = buff.WriteTo(c.w)
		if err != nil {
			return err
		}

		return c.w.Flush()
	}

	return buff, closerFunc(closer)
}
