package scope

import (
	"fmt"
	"hash"
	"hash/fnv"

	"k8s.io/apimachinery/pkg/util/dump"
)

// HashConfig returns the hash of the config. It is used as the key for the client cache.
func HashConfig(objectToWrite interface{}) uint64 {
	hash := fnv.New32a()
	DeepHashObject(hash, objectToWrite)
	return uint64(hash.Sum32())
}

// DeepHashObject writes specified object to hash using the spew library
// which follows pointers and prints actual values of the nested objects
// ensuring the hash does not change when a pointer changes.
// This function is taken from https://github.com/kubernetes/kubernetes/blob/v1.31.5/pkg/util/hash/hash.go#L26-L32
func DeepHashObject(hasher hash.Hash, objectToWrite interface{}) {
	hasher.Reset()
	fmt.Fprintf(hasher, "%v", dump.ForHash(objectToWrite))
}
