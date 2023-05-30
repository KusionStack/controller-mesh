/*
Copyright 2023 The KusionStack Authors.
Copyright 2021 The Kruise Authors.

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

package pool

var (
	BytesPool = &bytesPool{pool: make(chan []byte, 1000), baseWidth: 32 * 1024} // 32KB
)

type bytesPool struct {
	pool      chan []byte
	baseWidth int
}

func (bp *bytesPool) Get() (b []byte) {
	select {
	case b = <-bp.pool:
	// reuse existing buffer
	default:
		// create new buffer
		b = make([]byte, bp.baseWidth)
	}
	return
}

func (bp *bytesPool) Put(b []byte) {
	if cap(b) < bp.baseWidth {
		// someone tried to put back a too small buffer, discard it
		return
	}

	select {
	case bp.pool <- b:
	// bytes went back into pool
	default:
		// bytes didn't go back into pool, just discard
	}
}
