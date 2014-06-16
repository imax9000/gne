// Package supervisor contains implementation of simple process that keeps
// specified processes running by restarting them when they die. It can be
// useful if you just want to keep something running without any special
// handling of failures.
package supervisor

import (
	"fmt"

	"github.com/gelraen/gne"
)

// ChildSpec is the structure that contains everything supervisor should know
// about particular child.
type ChildSpec struct {
	Code func(*gne.Process)
	// TODO(imax): add restart policy and other stuff
}

// Supervisor spawns specified code in separate processes and restarts them if
// they die. If supervisor gets ExitMessage from some other process it will die
// (and send ExitMessage to all children). This allows to implement supervision
// trees.
type Supervisor struct {
	children map[*gne.Process]*ChildSpec
	config   []ChildSpec
}

// New creates Supervisor instance, but does not run it.
func New(config []ChildSpec) *Supervisor {
	return &Supervisor{config: config}
}

// Spawn actually spawns supervisor, which, in turn, spawns all it's children.
func (s *Supervisor) Spawn() *gne.Process {
	return gne.Spawn(s.run)
}

// SpawnAndLink does the same thing as Spawn, but also links supervisor's
// process to parent.
func (s *Supervisor) SpawnAndLink(parent *gne.Process) *gne.Process {
	return parent.SpawnAndLink(s.run)
}

func (s *Supervisor) run(proc *gne.Process) {
	s.children = map[*gne.Process]*ChildSpec{}
	for _, spec := range s.config {
		s.children[proc.SpawnAndLink(spec.Code)] = &spec
	}
	proc.SetTrapExit(true)
	for {
		m := proc.Receive()
		switch m.Data.(type) {
		case gne.ExitMessage:
			if spec, ok := s.children[m.From]; ok {
				delete(s.children, m.From)
				s.children[proc.SpawnAndLink(spec.Code)] = spec
			} else {
				gne.Exit(fmt.Sprintf("Got ExitMessage not from child: %+v", m))
			}
		default:
			break // do nothing yet
		}
	}
}
