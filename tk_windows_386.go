// Copyright 2024 The tk9.0-go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tk9_0 // import "modernc.org/tk9.0"

import (
	_ "embed"
)

var (
	//go:embed embed_windows_386/dll.zip
	dlls []byte

	interp uintptr
)