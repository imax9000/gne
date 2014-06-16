// Package gne contains the implementation of Erlang-like processes, that can
// be used to make error handling in concurrent system easier and helps to
// implement things that map to message passing paradigm naturally.
// "gne" stands for "Go is Not Erlang".
//
// This package is more focused on error handling concepts briefly described in
// http://www.erlang.org/course/error_handling.html .
// In short, processes are arranged in some structure (usually a tree) with
// two-way links; if one process dies - it notifies all processes linked to it,
// and by default linked processes also die. Crash propagation stops when it
// reaches a process that catches death notifications and does something more
// meaningful about them, like restarting the dead process with a clean state.
//
// This allows to write code mostly for normal flow of execution and handle all
// errors at the upper application layers and build application that can
// gracefully recover even from unforeseen errors.
package gne

import (
	"fmt"
	"runtime"
)

// An Envelope is the basic data structure that is passed between processes.
type Envelope struct {
	Data interface{}
	From *Process
}

// serviceMessage describes internal messages that require specific actions.
type serviceMessage interface {
	// Handle is called from Process.HandleMessage to do message-specific
	// actions. It should be called only within context of the goroutine
	// associated with Process supplied as a first argument. If the return value
	// is true, Process.HandleMessage will return message to its caller (like
	// user-supplied event loop), otherwise it will return nil.
	handle(*Process, *Envelope) bool
}

// A DeathReason structure wraps arbitrary data that should describe why
// process died. It's used by Exit function as an argument for panic().
type DeathReason struct {
	Data       interface{}
	StackTrace []byte
}

// Error is the part of builtin error interface.
func (d DeathReason) Error() string {
	return fmt.Sprintf("process died: %+v\nStack trace:\n%s", d.Data, string(d.StackTrace))
}

// Exit forces the current process to die by calling panic. It's used to
// minimize amount of error-handling code: in case of inconsistencies just call
// this method and let parent Process restart it in known good state.
func Exit(data interface{}) {
	var buf []byte
	runtime.Stack(buf, false)
	panic(DeathReason{Data: data, StackTrace: buf})
}
