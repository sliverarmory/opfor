package javaser

import (
	"bytes"
	"testing"
)

func FuzzDecode(f *testing.F) {
	f.Add([]byte{0xac, 0xed, 0, 5, TCNull})
	f.Add([]byte{0xac, 0xed, 0, 5, TCString, 0, 1, 'x'})
	f.Add([]byte{0xac, 0xed, 0, 5, TCReset, TCString, 0, 1, 'x'})

	limits := DefaultLimits()
	limits.MaxDepth = 64
	limits.MaxHandles = 10_000
	limits.MaxClassDescriptors = 1_000
	limits.MaxFieldsPerClass = 1_000
	limits.MaxTotalFields = 10_000
	limits.MaxProxyInterfaces = 1_000
	limits.MaxAnnotationItems = 10_000
	limits.MaxArrayLength = 100_000
	limits.MaxStringBytes = 1 << 20
	limits.MaxBlockBytes = 1 << 20
	limits.MaxTotalBytes = 2 << 20

	f.Fuzz(func(t *testing.T, input []byte) {
		value, err := Decode(
			bytes.NewReader(input),
			WithLimits(limits),
			WithClassDataResolver(sleepGraphLayout),
		)
		if err != nil {
			return
		}
		var output bytes.Buffer
		if err := Encode(&output, value, WithLimits(limits)); err != nil {
			return
		}
		_, _ = Decode(
			bytes.NewReader(output.Bytes()),
			WithLimits(limits),
			WithClassDataResolver(sleepGraphLayout),
		)
	})
}
