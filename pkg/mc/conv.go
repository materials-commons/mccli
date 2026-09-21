package mc

import (
	"strconv"
)

func ToInt64Ptr(i int64) *int64 {
	return &i
}

func ToStringPtr(value string) *string {
	return &value
}

func ToStringPtrWithNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func ToInt64PtrString(value *int64) string {
	if value == nil {
		return "-"
	}
	return strconv.FormatInt(*value, 10)
}
