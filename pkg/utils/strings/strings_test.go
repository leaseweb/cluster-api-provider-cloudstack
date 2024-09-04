/*
Copyright 2024 The Kubernetes Authors.

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

package strings_test

import (
	"slices"
	"testing"

	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/utils/strings"
)

func TestCanonicalize(t *testing.T) {
	tests := []struct {
		name  string
		value []string
		want  []string
	}{
		{
			name:  "Empty list",
			value: []string{},
			want:  []string{},
		},
		{
			name:  "Identity",
			value: []string{"a", "b", "c"},
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "Out of order",
			value: []string{"c", "b", "a"},
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "Duplicate elements",
			value: []string{"c", "b", "a", "c"},
			want:  []string{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Canonicalize(tt.value)
			if !slices.Equal(got, tt.want) {
				t.Errorf("CompareLists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSliceDiff(t *testing.T) {
	tests := []struct {
		name           string
		value1, value2 []string
		want1, want2   []string
	}{
		{
			name:   "Empty list",
			value1: []string{},
			value2: []string{},
			want1:  []string{},
			want2:  []string{},
		},
		{
			name:   "Same",
			value1: []string{"a", "b", "c"},
			value2: []string{"a", "b", "c"},
			want1:  []string{},
			want2:  []string{},
		},
		{
			name:   "one different",
			value1: []string{"a", "b", "c"},
			value2: []string{"a", "b", "c", "d"},
			want1:  []string{},
			want2:  []string{"d"},
		},
		{
			name:   "other different",
			value1: []string{"a", "b", "c", "d"},
			value2: []string{"a", "b", "c"},
			want1:  []string{"d"},
			want2:  []string{},
		},
		{
			name:   "both different",
			value1: []string{"a", "b", "c", "e"},
			value2: []string{"a", "b", "d", "f"},
			want1:  []string{"c", "e"},
			want2:  []string{"d", "f"},
		},
		{
			name:   "Duplicate elements",
			value1: []string{"c", "b", "a", "c"},
			value2: []string{"c", "b", "a", "c"},
			want1:  []string{},
			want2:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res1, res2 := strings.SliceDiff(tt.value1, tt.value2)
			if !slices.Equal(res1, tt.want1) {
				t.Errorf("CompareLists() = %v, want %v", res1, tt.want1)
			}
			if !slices.Equal(res2, tt.want2) {
				t.Errorf("CompareLists() = %v, want %v", res2, tt.want2)
			}
		})
	}
}
