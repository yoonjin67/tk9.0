// Copyright 2024 The tk9.0-go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && amd64

package tk9_0 // import "modernc.org/tk9.0"

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/evilsocket/islazy/zip"
	"modernc.org/memory"
)

const (
	tcl_eval_direct = 0x40000 // tcl9.0b3/generic/tcl.h:978
	tcl_ok          = 0       // tcl9.0b3/generic/tcl.h:522
	tcl_error       = 1       // tcl9.0b3/generic/tcl.h:523

)

var (
	// No mutex, the package must be used by a single goroutine only.
	allocator memory.Allocator

	createCommandProc uintptr
	evalExProc        uintptr
	getObjResultProc  uintptr
	getStringProc     uintptr
	interp            uintptr
	newStringObjProc  uintptr
	runCmdProxy       = purego.NewCallback(eventDispatcher)
	setObjResultProc  uintptr
	tclBinHandle      uintptr
	tkBinHandle       uintptr
)

func init() {
	if isBuilder {
		return
	}

	runtime.LockOSThread()
	var cacheDir string
	if cacheDir, Error = getCacheDir(); Error != nil {
		return
	}

	if init1(cacheDir); Error != nil {
		return
	}

	var nm uintptr
	if nm, Error = cString("eventDispatcher"); Error != nil {
		return
	}

	cmd, _, _ := purego.SyscallN(createCommandProc, interp, nm, runCmdProxy, 0, 0)
	if cmd == 0 {
		Error = fmt.Errorf("registering event dispatcher proxy failed: %v", getObjResultProc)
		return
	}

	setDefaults()
}

func init1(cacheDir string) {
	var wd string
	if wd, Error = os.Getwd(); Error != nil {
		return
	}

	defer func() {
		Error = errors.Join(Error, os.Chdir(wd))
	}()

	if Error = os.Chdir(cacheDir); Error != nil {
		return
	}

	if tclBinHandle, Error = purego.Dlopen(filepath.Join(cacheDir, tclBin), purego.RTLD_LAZY|purego.RTLD_GLOBAL); Error != nil {
		return
	}

	if tkBinHandle, Error = purego.Dlopen(filepath.Join(cacheDir, tkBin), purego.RTLD_LAZY|purego.RTLD_GLOBAL); Error != nil {
		return
	}

	var tclCreateInterpProc, tclInitProc, tkInitProc uintptr
	if tclCreateInterpProc, Error = purego.Dlsym(tclBinHandle, "Tcl_CreateInterp"); Error != nil {
		return
	}

	if tclInitProc, Error = purego.Dlsym(tclBinHandle, "Tcl_Init"); Error != nil {
		return
	}

	if createCommandProc, Error = purego.Dlsym(tclBinHandle, "Tcl_CreateCommand"); Error != nil {
		return
	}

	if evalExProc, Error = purego.Dlsym(tclBinHandle, "Tcl_EvalEx"); Error != nil {
		return
	}

	if setObjResultProc, Error = purego.Dlsym(tclBinHandle, "Tcl_SetObjResult"); Error != nil {
		return
	}

	if getObjResultProc, Error = purego.Dlsym(tclBinHandle, "Tcl_GetObjResult"); Error != nil {
		return
	}

	if getStringProc, Error = purego.Dlsym(tclBinHandle, "Tcl_GetString"); Error != nil {
		return
	}

	if newStringObjProc, Error = purego.Dlsym(tclBinHandle, "Tcl_NewStringObj"); Error != nil {
		return
	}

	if tkInitProc, Error = purego.Dlsym(tkBinHandle, "Tk_Init"); Error != nil {
		return
	}

	if interp, _, _ = purego.SyscallN(tclCreateInterpProc); interp == 0 {
		Error = fmt.Errorf("failed to create a Tcl interpreter")
		return
	}

	if r, _, _ := purego.SyscallN(tclInitProc, interp); r != tcl_ok {
		Error = fmt.Errorf("failed to initialize the Tcl interpreter")
		return
	}

	fn := filepath.Join(cacheDir, "libtk9.0.0.zip")
	if _, Error := eval(fmt.Sprintf("zipfs mount %s /app", fn)); Error != nil {
		return
	}

	if r, _, _ := purego.SyscallN(tkInitProc, interp); r != tcl_ok {
		Error = fmt.Errorf("failed to initialize Tk")
		return
	}
}

func getCacheDir() (r string, err error) {
	if r, err = os.UserCacheDir(); err != nil {
		return "", err
	}

	r0 := filepath.Join(r, "modernc.org")
	r = filepath.Join(r0, libVersion)
	fi, err := os.Stat(r)
	if err == nil && fi.IsDir() {
		return r, nil
	}

	err = os.MkdirAll(r0, 0700)
	tmp, err := os.MkdirTemp("", "tk9.0-")
	if err != nil {
		return "", err
	}

	zf := filepath.Join(tmp, "lib.zip")
	if err = os.WriteFile(zf, libZip, 0660); err != nil {
		return "", err
	}

	if _, err = zip.Unzip(zf, tmp); err != nil {
		os.Remove(zf)
		return "", err
	}

	os.Remove(zf)
	if err = os.Rename(tmp, r); err == nil {
		return r, nil
	}

	cleanupDirs = append(cleanupDirs, tmp)
	return tmp, nil
}

// Finalize releases all resources held, if any. This may include temporary
// files. Finalize is intended to be called on process shutdown only.
func Finalize() (err error) {
	if finished.Swap(1) != 0 {
		return
	}

	defer runtime.UnlockOSThread()

	for _, v := range cleanupDirs {
		err = errors.Join(err, os.RemoveAll(v))
	}
	return err
}

func eval(code string) (r string, err error) {
	if dmesgs {
		defer func() {
			dmesg("code=%s -> r=%v err=%v", code, r, err)
		}()
	}
	cs, err := cString(code)
	if err != nil {
		return "", err
	}

	defer allocator.UintptrFree(cs)

	if r0, _, _ := purego.SyscallN(evalExProc, interp, cs, uintptr(len(code)), tcl_eval_direct); r0 == tcl_ok {
		return tclResult(), nil
	}

	return "", fmt.Errorf("%s", tclResult())
}

func tclResult() string {
	r, _, _ := purego.SyscallN(getObjResultProc, interp)
	if r == 0 {
		return ""
	}

	if r, _, _ = purego.SyscallN(getStringProc, r); r != 0 {
		return goString(r)
	}

	return ""
}

func goString(p uintptr) string { // Result can be retained.
	if p == 0 {
		return ""
	}

	p0 := p
	var n int
	for ; *(*byte)(unsafe.Pointer(p)) != 0; n++ {
		p++
	}
	if n != 0 {
		return string(unsafe.Slice((*byte)(unsafe.Pointer(p0)), n))
	}

	return ""
}

func cString(s string) (r uintptr, err error) {
	if s == "" {
		return 0, nil
	}

	if r, err = allocator.UintptrMalloc(len(s) + 1); err != nil {
		return 0, err
	}

	copy(unsafe.Slice((*byte)(unsafe.Pointer(r)), len(s)), s)
	*(*byte)(unsafe.Add(unsafe.Pointer(r), len(s))) = 0
	return r, nil
}

func eventDispatcher(clientData, in uintptr, argc int32, argv uintptr) uintptr {
	if argc < 2 {
		setResult(fmt.Sprintf("eventDispatcher internal error: argc=%v", argc))
		return tcl_error
	}

	arg1 := goTransientString(*(*uintptr)(unsafe.Pointer(argv + unsafe.Sizeof(uintptr(0)))))
	id, err := strconv.Atoi(arg1)
	if err != nil {
		setResult(fmt.Sprintf("eventDispatcher internal error: argv[1]=%q, err=%v", arg1, err))
		return tcl_error
	}

	h := handlers[int32(id)]
	e := &Event{W: h.w}
	for i := int32(2); i < argc; i++ {
		e.args = append(e.args, goString(*(*uintptr)(unsafe.Pointer(argv + uintptr(i)*unsafe.Sizeof(uintptr(0))))))
	}
	switch h.callback(e); {
	case e.Err != nil:
		setResult(tclSafeString(e.Err.Error()))
		return tcl_error
	default:
		if setResult("") != nil {
			return tcl_error
		}

		return tcl_ok
	}
}

func goTransientString(p uintptr) (r string) { // Result cannot be retained.
	if p == 0 {
		return ""
	}

	var n uintptr
	for p := p; *(*byte)(unsafe.Pointer(p + n)) != 0; n++ {
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(p)), n))
}

func setResult(s string) (err error) {
	cs, err := cString(s)
	if err != nil {
		return err
	}

	defer allocator.UintptrFree(cs)

	obj, _, _ := purego.SyscallN(newStringObjProc, cs, uintptr(len(s)))
	if obj == 0 {
		return fmt.Errorf("OOM")
	}

	purego.SyscallN(setObjResultProc, interp, obj)
	return nil
}