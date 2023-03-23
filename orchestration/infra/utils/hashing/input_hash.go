package hashing

import "github.com/cespare/xxhash"

func MakeHash(inputData []byte) uint64 {
	return xxhash.Sum64(inputData)
}
