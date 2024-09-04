/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package strings

import (
	"cmp"
	"slices"
)

func Canonicalize[S ~[]E, E cmp.Ordered](s S) S {
	slices.Sort(s)

	return slices.Compact(s)
}

// SliceDiff determines the differences between two slices, returning the items in list1 that are not present in list2,
// and the items in list2 that are not present in list1.
func SliceDiff[T ~[]E, E comparable](list1 T, list2 T) (ret1 T, ret2 T) {
	m1 := map[E]struct{}{}
	m2 := map[E]struct{}{}
	for _, v := range list1 {
		m1[v] = struct{}{}
	}
	for _, v := range list2 {
		m2[v] = struct{}{}
	}

	ret1 = make(T, 0)
	ret2 = make(T, 0)
	for _, v := range list1 {
		if _, ok := m2[v]; !ok {
			ret1 = append(ret1, v)
		}
	}
	for _, v := range list2 {
		if _, ok := m1[v]; !ok {
			ret2 = append(ret2, v)
		}
	}

	return ret1, ret2
}
