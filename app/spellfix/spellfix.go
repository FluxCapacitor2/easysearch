package spellfix

// Modified from https://github.com/asg017/sqlite-vec-go-bindings/blob/main/cgo/lib.go

//go:generate ./generate.sh

// #cgo CFLAGS: -DSQLITE_CORE
// #cgo linux LDFLAGS: -lm
// #include "spellfix.h"
//
import "C"

// Once called, every future new SQLite3 connection created in this process
// will have the spellfix1 extension loaded. It will persist until [Cancel] is
// called.
//
// Calls [sqlite3_auto_extension()] under the hood.
//
// [sqlite3_auto_extension()]: https://www.sqlite.org/c3ref/auto_extension.html
func Auto() {
	C.sqlite3_auto_extension((*[0]byte)((C.sqlite3_spellfix_init)))
}

// "Cancels" any previous calls to [Auto]. Any new SQLite3 connections created
// will not have the spellfix1 extension loaded.
//
// Calls sqlite3_cancel_auto_extension() under the hood.
func Cancel() {
	C.sqlite3_cancel_auto_extension((*[0]byte)(C.sqlite3_spellfix_init))
}
