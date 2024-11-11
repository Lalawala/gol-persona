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

func (s *Server) ProcessWorld(req gol.Request, res *gol.Response) error {
	s.Turn = 0
	s.Resume = make(chan bool)
	mutex.Lock()
	s.World = copySlice(req.World)
	mutex.Unlock()
	s.Slice = make([][]byte, req.Parameter.ImageHeight)
	for i := range s.Slice {
		s.Slice[i] = make([]byte, req.Parameter.ImageWidth)
	}
	// TODO: Execute all turns of the Game of Life.
	if req.Parameter.Threads == 1 {
		chans := make(chan [][]byte)
		mutex.Lock()
		worldCopy := copySlice(s.World)
		go workers(req.Parameter, worldCopy, chans, req.Start, req.End)
		strip := <-chans
		s.Slice = copySlice(strip)
		s.Turn++
		mutex.Unlock()
	} else {
		sliceHeight := req.End - req.Start
		//Threads in each node
		var numThreads int
		if len(req.World) == 16 && len(req.World[0]) == 16 {
			numThreads = 4
		} else {
			numThreads = req.Parameter.Threads
		}
		chans := make([]chan [][]byte, numThreads)
		for i := 0; i < numThreads; i++ {
			chans[i] = make(chan [][]byte)
			// calculate the starting and ending point of the slice
			a := i * (sliceHeight / numThreads)
			b := (i + 1) * (sliceHeight / numThreads)
			// // in case of incomplete division, set the ending point of the last thread to the last line
			if i == numThreads-1 {
				b = sliceHeight
			}
			//to handle data race condition by passing a copy of world to goroutines
			mutex.Lock()
			worldCopy := copySlice(s.World)
			mutex.Unlock()
			// fmt.Println("a: ", a)
			// fmt.Println("b ", b)
			req.Parameter.ImageHeight += 2
			go workers(req.Parameter, worldCopy, chans[i], 1+a, 1+b)

		}
		s.CombinedSlice = copySlice(s.World)
		//combine all the strips produced by workers
		for i := 0; i < numThreads; i++ {
			// strip := <-chans[i]
			// startRow := i * (sliceHeight / numThreads)
			// for r, row := range strip {
			// 	mutex.Lock()
			// 	//replace each line of the old world by a new row
			// 	//fmt.Println("+", startRow+r)
			// 	s.CombinedSlice[startRow+r] = row
			// 	mutex.Unlock()
			// }
			worldPiece := <-chans[i]
			startRow := i * (sliceHeight / numThreads)
			for j := 0; j < len(worldPiece); j++ {
				copy(s.Slice[j+startRow], worldPiece[j])
			}
		}
		mutex.Lock()
		//copy the top slice to s.Slice
		// for i := 0; i < sliceHeight; i++ {
		// 	copy(s.Slice[i], s.CombinedSlice[1+i])
		// }
		//s.Slice = copySlice(s.CombinedSlice[1 : sliceHeight-1])
		s.Turn++
		mutex.Unlock()
	}
	//send the finished world and AliveCells to respond
	mutex.Lock()
	res.Slice = copySlice(s.Slice)
	mutex.Unlock()
	return nil
}

func (s *Server) KeyGol(req gol.Request, res *gol.Response) error {
	if req.K {
		os.Exit(0)
	}
	return nil
}

func nextState(p gol.Params, world [][]byte, start, end int) [][]byte {
	// allocate space
	// fmt.Println("end: ", end)
	// fmt.Println("start: ", start)
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
	// fmt.Println("start in worker: ", start)
	// fmt.Println("end in worker: ", end)
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

func main() {
	pAddr := flag.String("port", "8040", "port to listen on")
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
	listener, _ := net.Listen("tcp", ":"+*pAddr)
	defer func(listener net.Listener) {
		err := listener.Close()
		if err != nil {

		}
	}(listener)
	rpc.Accept(listener)
}
