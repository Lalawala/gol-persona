package main

import (
	"flag"
	"fmt"
	"math/rand"
	"net"
	"net/rpc"
	"sync"
	"time"
	"uk.ac.bris.cs/gameoflife/gol"
	"uk.ac.bris.cs/gameoflife/util"
)

var mutex sync.Mutex

// Create a RPC service that contains various
type Server struct {
	Turn          int
	CellCount     int
	Resume        chan bool
	Pause         bool
	World         [][]byte
	Slice         [][]byte
	CombinedSlice [][]byte
}

//TODO:IMPLEMENT THIS
func (s *Server) ProcessWorld(req gol.Request, res *gol.Response) error {
	//Initialization
	s.Turn = 0
	s.Resume = make(chan bool)

	// Lock to prevent race conditions, copy the world state from the request
	mutex.Lock()
	s.World = copySlice(req.World)
	mutex.Unlock()

	// Initialize the slice
	s.Slice = make([][]byte, req.Parameter.ImageHeight)
	for i := range s.Slice {
		s.Slice[i] = make([]byte, req.Parameter.ImageWidth)
	}

	sliceHeight := req.End - req.Start
	numThreads := req.Parameter.Threads

	// Adjust number of threads for small grids
	if len(req.World) == 16 && len(req.World[0]) == 16 {
		numThreads = 4
	}

	chans := make([]chan [][]byte, numThreads)
	for i := 0; i < numThreads; i++ {
		chans[i] = make(chan [][]byte)
		a := i * (sliceHeight / numThreads)
		b := (i + 1) * (sliceHeight / numThreads)
		if i == numThreads-1 {
			b = sliceHeight
		}

		//TODO:Launch worker goroutines
		go func(ch chan [][]byte, start, end int) {
			localWorldCopy := copySlice(req.World) // Each goroutine works on its own copy
			req.Parameter.ImageHeight += 2
			workers(req.Parameter, localWorldCopy, ch, 1+start, 1+end)
		}(chans[i], a, b)
	}

	// Collect results from each worker thread
	for i := 0; i < numThreads; i++ {
		worldPiece := <-chans[i]
		startRow := i * (sliceHeight / numThreads)
		for j := 0; j < len(worldPiece); j++ {
			copy(s.Slice[j+startRow], worldPiece[j])
		}
	}
	// Lock to prevent race conditions, update turn count
	mutex.Lock()
	s.Turn++
	mutex.Unlock()

	// Lock to prevent race conditions, copy the processed world state to the response
	mutex.Lock()
	res.Slice = copySlice(s.Slice)
	mutex.Unlock()

	return nil
}

func nextState(p gol.Params, world [][]byte, start, end int) [][]byte {
	// allocate space
	nextWorld := make([][]byte, end-start)
	for i := range nextWorld {
		nextWorld[i] = make([]byte, p.ImageWidth)
	}

	directions := [8][2]int{
		{-1, -1}, {-1, 0}, {-1, 1},
		{0, -1}, {0, 1},
		{1, -1}, {1, 0}, {1, 1},
	}

	for row := start; row < end; row++ {
		for col := 0; col < p.ImageWidth; col++ {
			// the alive must be set to 0 everytime when it comes to a different position
			alive := 0
			for _, dir := range directions {
				// + imageHeight make sure the image is connected
				newRow, newCol := (row+dir[0]+p.ImageHeight)%p.ImageHeight, (col+dir[1]+p.ImageWidth)%p.ImageWidth
				if world[newRow][newCol] == 255 {
					alive++
				}
			}
			if world[row][col] == 255 {
				if alive < 2 || alive > 3 {
					nextWorld[row-start][col] = 0
				} else {
					nextWorld[row-start][col] = 255
				}
			} else if world[row][col] == 0 {
				if alive == 3 {
					nextWorld[row-start][col] = 255
				} else {
					nextWorld[row-start][col] = 0
				}
			}
		}
	}
	return nextWorld
}

func workers(p gol.Params, world [][]byte, result chan<- [][]byte, start, end int) {
	worldPiece := nextState(p, world, start, end)
	result <- worldPiece
	close(result)
}

func copySlice(src [][]byte) [][]byte {
	dst := make([][]byte, len(src))
	for i := range src {
		dst[i] = make([]byte, len(src[i]))
		copy(dst[i], src[i])
	}
	return dst
}

func calculateAliveCells(p gol.Params, world [][]byte) []util.Cell {
	var aliveCell []util.Cell
	for row := 0; row < len(world); row++ {
		for col := 0; col < len(world[row]); col++ {
			if world[row][col] == 255 {
				aliveCell = append(aliveCell, util.Cell{X: col, Y: row})
			}
		}
	}
	return aliveCell
}

//TODO: NEED TO BE FIXED
func main() {
	pAddr := flag.String("port", "8070", "port to listen on")
	flag.Parse()
	//initialise server
	server := &Server{
		Resume:    make(chan bool),
		Turn:      0,
		CellCount: 0,
	}
	err := rpc.Register(server)
	if err != nil {
		return
	}
	flag.Parse()
	rand.Seed(time.Now().UnixNano())
	//rpc.Register(&Worker{hight: 0})
	listener, _ := net.Listen("tcp", ":"+*pAddr)
	defer func(listener net.Listener) {
		err := listener.Close()
		if err != nil {

		}
	}(listener)
	rpc.Accept(listener)
	fmt.Println("connected")
}
