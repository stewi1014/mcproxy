package protocol

import (
	"bytes"
	"testing"
)

func TestVarInt(t *testing.T) {
	var a, b VarInt
	var buff bytes.Buffer

	for i := range 4000 {
		a = VarInt(i)

		err := a.WriteBytesTo(&buff)
		if err != nil {
			panic(err)
		}

		err = b.ReadBytesFrom(&buff)
		if err != nil {
			panic(err)
		}

		if a != b {
			t.Fatal("mismatch. want", a, "but got", b)
		}
	}
}
