//go:build !linux

package collector

import "errors"

func queryI915MemRegions(string) ([]memRegion, error) { return nil, errors.ErrUnsupported }
