package conv

import (
	"strconv"
)

func Int64Ptr(i int64) *int64 {
	return &i
}

func StringPtr(value string) *string {
	return &value
}

func StringPtrWithNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func Int64PtrString(value *int64) string {
	if value == nil {
		return "-"
	}
	return strconv.FormatInt(*value, 10)
}
