package main

import (
	"flag"
	"net"
	"net/rpc"
	"os"
	"sync"
	"uk.ac.bris.cs/gameoflife/gol"

	"uk.ac.bris.cs/gameoflife/util"
)

var mutex sync.Mutex

type Broker struct {
	Pause              bool
	Resume             chan bool
	Turn               int
	CellCount          int
	Clients            []*rpc.Client
	CombinedWorld      [][]byte
	CombinedAliveCells []util.Cell
}

func (b *Broker) GolInitializer(req gol.Request, res *gol.Response) error {

	/*
		TODO:The GolInitializer function in the Broker serves several key purposes:
			1. Initialization:
			Sets up the initial state of the game world.
			Prepares data structures to store the combined world state and other necessary information.


			2. Task Distribution:
			Divides the game world into segments and assigns each segment to a different GOL worker.
			Ensures that each worker receives the correct portion of the world, including halo rows for boundary conditions.
			3. Synchronization:
			Manages synchronization between the Local Controller and GOL workers.
			Handles pause and resume functionality, allowing the game to be paused and continued based on user input.
			4. Result Collection:
			Collects the results from each GOL worker after processing.
			Combines these results to update the overall game world state.
			Finalization:
			Updates the response with the final state of the world, including the list of alive cells and the completion status.
			Overall, GolInitializer is responsible for orchestrating the initial setup and iterative processing of the Game of Life across distributed workers.

	*/
	// Initialize the game world and distribute tasks to GOL workers

	// Synchronization
	go func() {
		for {
			if b.Pause {
				<-b.Resume
			}
			// Continue processing
		}
	}()

	b.Resume = make(chan bool)
	b.CombinedWorld = make([][]byte, req.Parameter.ImageHeight)
	for i := range b.CombinedWorld {
		b.CombinedWorld[i] = make([]byte, req.Parameter.ImageWidth)
	}
	// Finalize
	res.World = copySlice(b.CombinedWorld)
	res.AliveCells = calculateAliveCells(req.Parameter, b.CombinedWorld)
	res.End = true
	return nil

	return nil
}

func (b *Broker) GolAliveCells(req gol.Request, res *gol.Response) error {
	mutex.Lock()
	defer mutex.Unlock()
	return nil
}

func (b *Broker) GolKey(req gol.Request, res *gol.Response) error {
	var wg sync.WaitGroup
	if req.S {
		mutex.Lock()
		res.Turns = b.Turn
		res.World = copySlice(b.CombinedWorld)
		mutex.Unlock()
	} else if req.P {
		mutex.Lock()
		b.Pause = !b.Pause
		mutex.Unlock()
		if !b.Pause {
			b.Resume <- true
		}
	} else if req.K {
		for _, client := range b.Clients {
			wg.Add(1)
			go func(client *rpc.Client) {
				defer wg.Done()
				client.Call(gol.Key, req, res)
			}(client)
		}
		wg.Wait()
		os.Exit(0)
	}
	return nil

}

func calculateAliveCells(p gol.Params, world [][]byte) []util.Cell {
	var cells []util.Cell
	for row := 0; row < p.ImageHeight; row++ {
		for col := 0; col < p.ImageWidth; col++ {
			if world[row][col] == 255 {
				cells = append(cells, util.Cell{X: col, Y: row})
			}
		}
	}
	return cells
}

func copySlice(src [][]byte) [][]byte {
	dst := make([][]byte, len(src))
	for i := range src {
		dst[i] = make([]byte, len(src[i]))
		copy(dst[i], src[i])
	}
	return dst
}

/*TODO:
1.Client Connections:
Establishes RPC connections to multiple GOL workers using their respective addresses.
These connections allow the Broker to distribute tasks and collect results from the workers.

2.Broker Initialization:
Creates a Broker instance, initializing its fields such as Clients, Resume, Turn, and CellCount.
The Resume channel is used to manage pause and resume functionality.

3. RPC Registration:
Registers the Broker instance as an RPC service, enabling it to handle incoming RPC calls from the Local Controller.

4.Listener Setup:
Sets up a TCP listener on a specified port (e.g., 8030) to accept incoming connections from the Local Controller.
This allows the Local Controller to communicate with the Broker via RPC.

5.RPC Acceptance:
Continuously accepts and serves incoming RPC requests from the Local Controller.
Overall, the main function sets up the necessary infrastructure for the Broker to function as an intermediary between the Local Controller and the GOL workers.
*/
func main() {
	// Broker Initialization
	broker := &Broker{
		Resume:    make(chan bool),
		Turn:      0,
		CellCount: 0,
	}

	// Register the broker
	if err := rpc.Register(broker); err != nil {
		return
	}

	// Parse the port flag
	port := flag.String("port", "8030", "port to listen on")
	flag.Parse()

	// Create a listener
	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	// Accept RPC connections
	rpc.Accept(listener)
}
