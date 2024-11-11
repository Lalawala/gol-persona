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

// Global mutex for thread safety
var mutex sync.Mutex

// Server represents the Game of Life engine
// Responsible for processing the game logic
type Server struct {
	Turn      int
	CellCount int
	Resume    chan bool
	Pause     bool
	World     [][]byte
}

// ProcessWorld handles a single blocking RPC call to process all turns
// req contains the initial world state and parameters
// res will contain the final world state and statistics
func (s *Server) ProcessWorld(req gol.Request, res *gol.Response) error {
	// Initialize server state
	s.Turn = 0
	s.Resume = make(chan bool)
	mutex.Lock()
	s.World = copySlice(req.World)
	mutex.Unlock()

	// Process all turns sequentially
	for ; s.Turn < req.Parameter.Turns; s.Turn++ {
		mutex.Lock()
		if s.Pause {
			mutex.Unlock()
			<-s.Resume
		} else {
			mutex.Unlock()
		} //avoiding race condition
		s.World = nextState(req.Parameter, s.World, 0, req.Parameter.ImageHeight)
		mutex.Lock()
		s.CellCount = len(calculateAliveCells(req.Parameter, s.World))
		s.Turn++
		mutex.Unlock()
	}
	if req.Parameter.Threads == 1 {
		s.World = nextState(req.Parameter, s.World, 0, req.Parameter.ImageHeight)
	} else {
		//TODO: NEEDED TO BE OPTIMISED
		// create a channel
		chans := make([]chan [][]byte, req.Parameter.Threads)
		for i := 0; i < req.Parameter.Threads; i++ {
			chans[i] = make(chan [][]byte)
			a := i * (req.Parameter.ImageHeight / req.Parameter.Threads)
			b := (i + 1) * (req.Parameter.ImageHeight / req.Parameter.Threads)
			if i == req.Parameter.Threads-1 {
				b = req.Parameter.ImageHeight
			}
			mutex.Lock()
			worldCopy := copySlice(s.World)
			mutex.Unlock()
			go workers(req.Parameter, worldCopy, chans[i], a, b)

		}
		//combine all the strips produced by workers
		for i := 0; i < req.Parameter.Threads; i++ {
			strip := <-chans[i]
			startRow := i * (req.Parameter.ImageHeight / req.Parameter.Threads)
			for r, row := range strip {
				mutex.Lock()
				req.World[startRow+r] = row
				mutex.Unlock()
			}
		}
		mutex.Lock()
		s.World = copySlice(req.World)
		mutex.Unlock()
	}
	// Prepare the response with final state
	mutex.Lock()
	res.World = s.World
	res.Turns = s.Turn
	res.AliveCells = calculateAliveCells(req.Parameter, s.World)
	mutex.Unlock()
	return nil
}

func (s *Server) CountAliveCell(req gol.Request, res *gol.Response) error {
	// Return the current number of alive cells
	mutex.Lock()
	res.Turns = s.Turn
	res.CellCount = s.CellCount
	mutex.Unlock()
	return nil
}

func (s *Server) KeyGol(req gol.Request, res *gol.Response) error {
	if req.S {
		mutex.Lock()
		res.Turns = s.Turn
		res.World = s.World
		mutex.Unlock()
	} else if req.P {
		mutex.Lock()
		s.Pause = !s.Pause
		mutex.Unlock()
		if !s.Pause {
			s.Resume <- true
		}
	} else if req.K {
		os.Exit(0)
	}
	return nil
}

// nextState calculates the next state of the world according to Game of Life rules
// Parameters:
// - p: game parameters including world dimensions
// - world: current world state
// - start, end: range of rows to process
// Returns the next state of the world
func nextState(p gol.Params, world [][]byte, start, end int) [][]byte {
	newWorld := make([][]byte, end-start)
	for i := range newWorld {
		newWorld[i] = make([]byte, p.ImageWidth)
	}
	//define the relative 8 position of the 8 neighboring cells
	directions := [8][2]int{
		{-1, -1}, {-1, 0}, {-1, 1},
		{0, -1}, {0, 1},
		{1, -1}, {1, 0}, {1, 1},
	}

	// Iterate over each row in the specified range
	for row := start; row < end; row++ {
		// Iterate over each column in the current row
		for col := 0; col < p.ImageWidth; col++ {
			alive := 0 // Counter for alive neighbors

			// Check all 8 neighboring cells
			for _, dir := range directions {
				// Calculate the position of the neighboring cell
				newRow := (row + dir[0] + p.ImageHeight) % p.ImageHeight
				newCol := (col + dir[1] + p.ImageWidth) % p.ImageWidth
				// Check if the neighboring cell is alive
				if world[newRow][newCol] == 255 {
					alive++
				}
			}

			// Apply the Game of Life rules
			if world[row][col] == 255 {
				if alive < 2 || alive > 3 {
					newWorld[row-start][col] = 0
				} else {
					newWorld[row-start][col] = 255
				}
			} else if world[row][col] == 0 {
				if alive == 3 {
					newWorld[row-start][col] = 255
				} else {
					newWorld[row-start][col] = 0
				}
			}
		}
	}
	return newWorld
}

// calculateAliveCells returns a slice of coordinates for all live cells
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

func workers(p gol.Params, world [][]byte, result chan<- [][]byte, start, end int) {
	worldPiece := nextState(p, world, start, end)
	result <- worldPiece
	close(result)
}

// copySlice creates a deep copy of a 2D byte slice
func copySlice(src [][]byte) [][]byte {
	dst := make([][]byte, len(src))
	for i := range src {
		dst[i] = make([]byte, len(src[i]))
		copy(dst[i], src[i])
	}
	return dst
}

func main() {
	// Parse command line arguments
	pAddr := flag.String("port", "8080", "Port to listen on")
	flag.Parse()

	// Create and register the RPC server
	server := new(Server)
	rpc.Register(server)

	// Start listening for connections
	listener, err := net.Listen("tcp", ":"+*pAddr)
	if err != nil {
		return
	}
	defer listener.Close()

	// Accept and serve RPC requests
	rpc.Accept(listener)
}
