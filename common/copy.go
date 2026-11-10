package common

import (
	"bytes"
	"fmt"

	"github.com/jinzhu/copier"
)

func DeepCopy[T any](src *T) (*T, error) {
	if src == nil {
		return nil, fmt.Errorf("copy source cannot be nil")
	}
	var dst T
	err := copier.CopyWithOption(&dst, src, copier.Option{
		DeepCopy:    true,
		IgnoreEmpty: true,
		Converters: []copier.TypeConverter{{
			SrcType: RawMessage{},
			DstType: RawMessage{},
			Fn: func(src any) (any, error) {
				// Preserve nil JSON values and isolate bytes between channel attempts.
				return RawMessage(bytes.Clone(src.(RawMessage))), nil
			},
		}},
	})
	if err != nil {
		return nil, err
	}
	return &dst, nil
}
