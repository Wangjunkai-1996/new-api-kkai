package common

import (
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
				// Raw JSON must stay independent without reflecting over every byte.
				// Keep copier's capacity and non-nil empty slice behavior.
				raw := src.(RawMessage)
				cloned := make(RawMessage, len(raw), cap(raw))
				copy(cloned, raw)
				return cloned, nil
			},
		}},
	})
	if err != nil {
		return nil, err
	}
	return &dst, nil
}
