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

package cloud

import (
	"bytes"
	"compress/gzip"

	"github.com/pkg/errors"
)

type (
	set      func(string)
	setArray func([]string)
	setInt   func(int64)
)

// setIfNotEmpty sets the given string to the setFn if it is not empty.
func setIfNotEmpty(str string, setFn set) {
	if str != "" {
		setFn(str)
	}
}

// setArrayIfNotEmpty sets the given array to the setFn if it is not empty.
func setArrayIfNotEmpty(strArray []string, setFn setArray) {
	if len(strArray) > 0 {
		setFn(strArray)
	}
}

// setIntIfPositive sets the given int64 to the setFn if it is greater than 0.
func setIntIfPositive(num int64, setFn setInt) {
	if num > 0 {
		setFn(num)
	}
}

// GzipBytes will gzip a byte array.
func GzipBytes(dat []byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(dat); err != nil {
		return []byte{}, errors.Wrap(err, "failed to gzip bytes")
	}

	if err := gz.Close(); err != nil {
		return []byte{}, errors.Wrap(err, "failed to gzip bytes")
	}

	return buf.Bytes(), nil
}
