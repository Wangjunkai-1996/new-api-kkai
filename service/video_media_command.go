package service

import "bytes"

const videoMediaCommandOutputLimit = 1 << 20

type boundedVideoMediaCommandOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (output *boundedVideoMediaCommandOutput) Write(content []byte) (int, error) {
	remaining := videoMediaCommandOutputLimit - output.buffer.Len()
	if remaining > 0 {
		written := len(content)
		if written > remaining {
			written = remaining
		}
		_, _ = output.buffer.Write(content[:written])
	}
	if len(content) > remaining {
		output.overflow = true
	}
	return len(content), nil
}

func (output *boundedVideoMediaCommandOutput) Bytes() []byte {
	return output.buffer.Bytes()
}
