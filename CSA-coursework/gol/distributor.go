package gol

import (
	"fmt"
	"net/rpc"
	"os"
	"sync"
	"time"
)

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
	key        <-chan rune
}

var wg sync.WaitGroup

func distributor(p Params, c distributorChannels) {
	// Initialize IO
	c.ioCommand <- ioInput
	c.ioFilename <- fmt.Sprintf("%dx%d", p.ImageWidth, p.ImageHeight)

	// Create initial world
	world := make([][]byte, p.ImageHeight)
	for i := range world {
		world[i] = make([]byte, p.ImageWidth)
	}

	// Read initial state
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			world[y][x] = <-c.ioInput
		}
	}

	// Connect to GOL server
	client, err := rpc.Dial("tcp", "127.0.0.1:8080")
	if err != nil {
		fmt.Printf("Failed to connect to GOL server: %v\n", err)
		return
	}
	defer client.Close()

	// Create request and response objects
	request := Request{
		World:     world,
		Parameter: p,
	}
	response := new(Response)

	// Set up ticker for alive cells count
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Start a goroutine to periodically request alive cell count
	go func() {
		for range ticker.C {
			countResp := new(Response)
			err := client.Call("Server.CountAliveCell", request, countResp)
			if err != nil {
				fmt.Printf("CountAliveCell error: %v\n", err)
				continue
			}
			// Send alive cells count event
			c.events <- AliveCellsCount{
				CompletedTurns: countResp.Turns,
				CellsCount:     countResp.CellCount,
			}
		}
	}()

	// Handle keypress events
	go func() {
		for {
			select {
			case key := <-c.key:
				switch key {
				case 's':
					request.S = true
					err := client.Call("Server.KeyGol", request, response)
					if err != nil {
						return
					}

					outputPGM(c, response.World, p, response.Turns)
				case 'q':
					c.events <- StateChange{response.Turns, Quitting}
					return
				case 'p':
					request.P = true
					err := client.Call("Server.KeyGol", request, response)
					if err != nil {
						return
					}
					if response.Turns%2 == 0 {
						fmt.Println("Paused at turn:", response.Turns)
					} else {
						fmt.Println("Continuing")
					}
				case 'k':
					request.K = true
					err := client.Call("Server.KeyGol", request, response)
					if err != nil {
						return
					}
					outputPGM(c, response.World, p, response.Turns)
					os.Exit(0)
				}
			}
		}
	}()

	// Make the main RPC call to process all turns
	err = client.Call("Server.ProcessWorld", request, response)
	if err != nil {
		fmt.Printf("ProcessWorld error: %v\n", err)
		return
	}

	// Wait for completion
	// Output final state
	c.ioCommand <- ioOutput
	outputFilename := fmt.Sprintf("%dx%dx%d", p.ImageWidth, p.ImageHeight, p.Turns)
	c.ioFilename <- outputFilename

	// Write the world state to the PGM file
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			c.ioOutput <- response.World[y][x]
		}
	}

	// Send final events
	c.events <- FinalTurnComplete{
		CompletedTurns: response.Turns,
		Alive:          response.AliveCells,
	}

	//// Notify that image output is complete
	//c.events <- ImageOutputComplete{
	//	CompletedTurns: p.Turns,
	//	Filename:       outputFilename,
	//}

	// Output the final state as PGM
	outputPGM(c, response.World, p, response.Turns)

	// Ensure IO is complete
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{response.Turns, Quitting}
	close(c.events)
}

//func handleKeyPress(k rune, client *rpc.Client, p Params, c distributorChannels) {
//
//}
func outputPGM(c distributorChannels, world [][]byte, p Params, turns int) {
	// Send the command to start output
	c.ioCommand <- ioOutput

	// Format the filename with dimensions and turns
	fileName := fmt.Sprintf("%dx%dx%d", p.ImageWidth, p.ImageHeight, turns)
	c.ioFilename <- fileName

	// Use a goroutine to handle the output asynchronously
	go func() {
		// Output the world state to the ioOutput channel
		for y := 0; y < p.ImageHeight; y++ {
			for x := 0; x < p.ImageWidth; x++ {
				c.ioOutput <- world[y][x]
			}
		}

		// Notify that the image output is complete
		c.events <- ImageOutputComplete{CompletedTurns: turns, Filename: fileName}
	}()
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
