package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/gelraen/gne"
)

func mainProc(self *gne.Process) {
	// start child process
	self.SpawnAndLink(func(proc *gne.Process) {
		// send some messages to parent process and exit
		proc.Send(self, "Hello!")
		proc.Send(self, "Hello!")
		proc.Send(self, "Hello!")
	})
	defer fmt.Println("mainProc finished.")
	// print all incoming messages
	// process will be killed when it receives ExitMessage from child
	for {
		msg := self.Receive()
		fmt.Println(msg.Data.(string))
	}
}

func exampleWithOneChild() {
	gne.WaitFor(gne.Spawn(mainProc))
	fmt.Println("main finished.")
	// Output:
	// Hello!
	// Hello!
	// Hello!
	// mainProc finished.
	// main finished.
}

func worker(proc *gne.Process) {
	fmt.Println("Worker started")
	defer fmt.Println("Worker died")
	for {
		msg := proc.Receive()
		switch data := msg.Data.(type) {
		case int:
			fmt.Println("Got something to do: ", data)
			if data%2 == 1 && rand.Float32() >= 0.7 {
				fmt.Println("Oh noes, critical error, exiting.")
				gne.Exit(msg)
			}
		}
	}
}

func server(proc *gne.Process) {
	proc.SetTrapExit(true)
	fmt.Println("Starting server")
	defer fmt.Println("Server finished")
	// Spawn pool of workers.
	const numWorkers = 10
	// Arranging data structures for finding our workers easily.
	workerToID := map[*gne.Process]int{}
	idToWorker := make([]*gne.Process, numWorkers)
	for i := 0; i < numWorkers; i++ {
		idToWorker[i] = proc.SpawnAndLink(worker)
		workerToID[idToWorker[i]] = i
	}

	nextWorker := 0
	// Run main event loop.
	for {
		msg := proc.Receive()
		switch data := msg.Data.(type) {
		case gne.ExitMessage:
			if i, ok := workerToID[msg.From]; ok {
				// One of our workers died, so let's restart it.
				fmt.Println("Restarting worker ", i)
				delete(workerToID, msg.From)
				idToWorker[i] = proc.SpawnAndLink(worker)
				workerToID[idToWorker[i]] = i
			} else {
				// We got ExitMessage from some process other than our worker, most
				// probably from parent process, so we should terminate in this case.
				gne.Exit(msg)
			}
		case int:
			// Delegate incoming request to one of workers.
			proc.Send(idToWorker[nextWorker], data)
			nextWorker++
			if nextWorker >= numWorkers {
				nextWorker = 0
			}
		}
	}
}

func client(proc *gne.Process, serverProc *gne.Process) {
	for i := 0; i < 50; i++ {
		proc.Send(serverProc, i)
		time.Sleep(100 * time.Millisecond)
	}
}

// Workers in this example are long-lived processes that may have some state
// and spawn some additional processes. In case of failure each subtree is
// restarted appropritely. Client process is linked to server just for
// simplicity: when client dies, it sends ExitMessage to server and server will
// die too.
func exampleWithPoolOfWorkersThatDieSometimes() {
	serverProc := gne.Spawn(server)
	serverProc.SpawnAndLink(func(proc *gne.Process) {
		client(proc, serverProc)
	})
	gne.WaitFor(serverProc)
}

func main() {
	exampleWithOneChild()
	exampleWithPoolOfWorkersThatDieSometimes()
}
