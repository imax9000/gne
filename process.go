package gne

import (
	"fmt"
	"runtime"
	"sync"
)

// envelopeBufferSize defines size of process' input channel.
const envelopeBufferSize = 100 // Arbitrary number

// ProcessState is a enum type for describing current process state.
type ProcessState int

const (
	// Running means that process is currently alive and running
	Running ProcessState = iota
	// Stopped means that process have died and reference to its Process
	// structure should be removed
	Stopped
)

// Envelope types

// ExitMessage indicates death of a process. ExitMessage is sent to each process
// in Process.links during process shutdown.
type ExitMessage struct {
	Reason *DeathReason
}

// handle behaviour depends on value of Process' TrapExit flag. If flag
// set to false (which is the default) process will die, otherwise, sender will
// be removed from links and ExitMessage will be returned by Process.Receive or
// Process.HandleMessage to user's code.
func (ExitMessage) handle(proc *Process, e *Envelope) bool {
	if proc.TrapExit() {
		proc.removeFromLinks(e.From)
		return true
	}
	Exit(*e)
	return false
}

// initiateLinkMessage starts procedure of creating a link between 2 processes.
type initiateLinkMessage struct {
	Target *Process
}

// HandleMessage adds Target to Process.links, so current process will notify
// Target about its death, and sends linkMessage to Target. If Target is
// already dead it puts "fake" ExitMessage into input queue of current process.
func (m initiateLinkMessage) handle(proc *Process, _ *Envelope) (result bool) {
	success := func() bool {
		m.Target.Lock()
		defer m.Target.Unlock()
		if m.Target.state == Running {
			proc.Send(m.Target, linkMessage{})
			return true
		}
		return false
	}()
	if !success {
		proc.Lock()
		defer proc.Unlock()
		if proc.state == Running {
			proc.addToLinks(m.Target)
			proc.channel <- Envelope{
				From: m.Target,
				Data: ExitMessage{},
			}
		}
	}
	return false
}

// linkMessage is sent by a process to initiateLinkMessage.Target.
type linkMessage struct {
}

// handle adds Envelope sender to Process.links.
func (linkMessage) handle(proc *Process, e *Envelope) bool {
	proc.addToLinks(e.From)
	return false
}

type killMessage struct {
}

func (killMessage) handle(proc *Process, e *Envelope) bool {
	proc.removeFromLinks(e.From)
	Exit(e)
	return false
}

// Error types

// DeadProcessError is returned when remote process is already dead.
type DeadProcessError struct {
	Proc    *Process
	Comment string
}

func (err *DeadProcessError) Error() string {
	return fmt.Sprintf("process %+v is dead: %s", *err.Proc, err.Comment)
}

// Process structure describes the process itself.
type Process struct {
	channel    chan Envelope
	links      map[*Process]bool
	state      ProcessState
	trapExit   bool
	sync.Mutex // any access to links, state and trapExit should be done while holding Mutex
}

// Exported functions

// Channel returns process' input channel.
func (p *Process) Channel() chan Envelope {
	return p.channel
}

// State returns current state of the process.
func (p *Process) State() ProcessState {
	p.Lock()
	defer p.Unlock()
	return p.state
}

// TrapExit returns current value of trapExit flag. See SetTrapExit and
// ExitMessage
func (p *Process) TrapExit() bool {
	p.Lock()
	defer p.Unlock()
	return p.trapExit
}

// SetTrapExit sets vaule of trapExit flag. If it's true, incoming ExitMessages
// will not kill the process, but just remove sender from Process.links and
// ExitMessage will be returned to user's code by handle or Receive.
func (p *Process) SetTrapExit(value bool) bool {
	p.Lock()
	defer p.Unlock()
	old := p.trapExit
	p.trapExit = value
	return old
}

// Send wraps arbitrary data into Envelope and puts it into input channel of
// other. If channel is closed message will be silently discarded.
func (p *Process) Send(other *Process, data interface{}) {
	p.sendEnvelope(other, Envelope{Data: data})
}

// HandleMessage handles service Messages. Returns e if it's not service
// Envelope or if it's ExitMessage and TrapExit was set to true. Returns nil
// otherwise.
func (p *Process) HandleMessage(e *Envelope) *Envelope {
	switch e.Data.(type) {
	case serviceMessage:
		if e.Data.(serviceMessage).handle(p, e) {
			return e
		}
		return nil
	}
	return e
}

// Receive returns next incoming Envelope.
func (p *Process) Receive() *Envelope {
	for m := range p.channel {
		if newMsg := p.HandleMessage(&m); newMsg != nil {
			return newMsg
		}
	}
	return nil
}

// Spawn creates a new process and runs it in separate goroutine.
func Spawn(body func(*Process)) *Process {
	proc := Process{
		channel: make(chan Envelope, envelopeBufferSize),
		links:   map[*Process]bool{},
		state:   Running,
	}
	go run(&proc, body)
	return &proc
}

// SpawnAndLink creates new process linked to current one and runs it in
// seprarte goroutine.
func (p *Process) SpawnAndLink(body func(*Process)) *Process {
	newProc := Process{
		channel: make(chan Envelope, envelopeBufferSize),
		links:   map[*Process]bool{p: true},
		state:   Running,
	}
	p.addToLinks(&newProc)
	go run(&newProc, body)
	return &newProc
}

// Link creates a link between 2 processes, so each of them will be notified
// about other's death.
func (p *Process) Link(other *Process) error {
	p.Lock()
	defer p.Unlock()
	if p.state != Running {
		return &DeadProcessError{
			Proc:    p,
			Comment: "dead processes cannot link to others",
		}
	}
	p.channel <- Envelope{Data: initiateLinkMessage{Target: other}}
	return nil
}

// Kill removes other from list of links and sends message to other that will
// cause it to die. Returns DeadProcessError if other is already dead.
func (p *Process) Kill(other *Process) error {
	p.removeFromLinks(other)
	return p.sendEnvelope(other, Envelope{Data: killMessage{}})
}

// WaitFor blocks until Process exits.
func WaitFor(proc *Process) {
	channel := make(chan int)
	proc.SpawnAndLink(func(waiter *Process) {
		for m := range waiter.channel {
			// we do not call HandleMessage() because all we want is to just wait while proc exits
			if _, ok := m.Data.(ExitMessage); ok && m.From == proc {
				break
			}
		}
		channel <- 0
	})
	<-channel
}

// Helper functions

func (p *Process) sendEnvelope(other *Process, e Envelope) (ret error) {
	e.From = p
	defer onWriteToClosedChannel(func() {ret = &DeadProcessError{Proc: other}})
	other.channel <- e
	return
}

func (p *Process) sendExitSignal(reason *DeathReason) {
	for other := range p.links {
		// here we need to ignore _all_ panics
		ignorePanic(func() {
			p.Send(other, ExitMessage{Reason: reason})
		})
	}
}

func ignorePanic(code func()) {
	defer func() {
		recover()
	}()
	code()
}

func run(proc *Process, body func(*Process)) {
	defer handleDeath(proc)
	body(proc)
}

func handleDeath(proc *Process) {
	proc.Lock()
	proc.state = Stopped
	close(proc.channel)
	proc.Unlock()
	for e := range proc.channel {
		if lm, ok := e.Data.(initiateLinkMessage); ok {
			lm.handle(proc, &e)
		}
		if lm, ok := e.Data.(linkMessage); ok {
			lm.handle(proc, &e)
		}
		if km, ok := e.Data.(killMessage); ok {
			ignorePanic(func() {
				km.handle(proc, &e)
			})
		}
	}
	if e := recover(); e != nil {
		var buf []byte
		runtime.Stack(buf, false)
		proc.sendExitSignal(&DeathReason{Data: e, StackTrace: buf})
	} else {
		proc.sendExitSignal(nil)
	}
}

func (p *Process) addToLinks(other *Process) {
	p.Lock()
	defer p.Unlock()
	if _, ok := p.links[other]; !ok {
		p.links[other] = true
	}
}

func (p *Process) removeFromLinks(other *Process) {
	p.Lock()
	defer p.Unlock()
	delete(p.links, other)
}

func onWriteToClosedChannel(cb func()) {
	if e := recover(); e != nil {
		if err := e.(error); err != nil {
			// *sigh*
			if err.Error() == "runtime error: send on closed channel" {
				cb()
				return
			}
		}
		panic(e)
	}
}
