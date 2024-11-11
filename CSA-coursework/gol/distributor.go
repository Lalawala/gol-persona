package gol

import (
	"fmt"
	"net/rpc"
	"os"
	"sync"
	"time"
	"uk.ac.bris.cs/gameoflife/util"
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
	//var mutex sync.Mutex
	kill := false
	pause := false
	// Initialize IO
	c.ioCommand <- ioInput
	c.ioFilename <- fmt.Sprintf("%dx%d", p.ImageWidth, p.ImageHeight)

	broker := "12.7.0.0.1:8080"
	// Create initial world
	world := make([][]byte, p.ImageHeight)
	for i := range world {
		world[i] = make([]byte, p.ImageWidth)
	}

	// Close the dial when everything is executed
	client, _ := rpc.Dial("tcp", broker)
	defer func() {
		if err := client.Close(); err != nil {
			fmt.Println("Error closing RPC client:", err)
		}
	}()

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

	//TODO: Whether need to add mutex or  else neede to be fixed?
	// Start a goroutine to periodically request alive cell count

	go func() {
		for range ticker.C {
			// Check if the process is not paused or killed
			if !pause && !kill {
				err := client.Call(BrokerAliveCells, request, &response)
				if err != nil {
					return
				}
				// Send an event with the current number of alive cells and completed turns
				c.events <- AliveCellsCount{
					CompletedTurns: response.Turns,
					CellsCount:     response.CellCount}
			}
		}
	}()

	//go func() {
	//	for range ticker.C {
	//		countResp := new(Response)
	//		err := client.Call(Live, request, countResp)
	//		if err != nil {
	//			fmt.Printf("CountAliveCell error: %v\n", err)
	//			continue
	//		}
	//		// Send alive cells count event
	//		c.events <- AliveCellsCount{
	//			CompletedTurns: countResp.Turns,
	//			CellsCount:     countResp.CellCount,
	//		}
	//	}
	//}()

	newTicker := time.NewTicker(50 * time.Millisecond)
	defer newTicker.Stop()
	previousWorld := copySlice(world)
	go func() {
		for range newTicker.C {
			request := Request{World: previousWorld}
			err := client.Call(Live, request, &response)
			defer client.Close()
			if err != nil {
				fmt.Println(err)
			}
			// Compare the previous world state with the new state
			for i := 0; i < p.ImageHeight; i++ {
				for j := 0; j < p.ImageWidth; j++ {
					// If a cell has changed, send a CellFlipped event
					if previousWorld[i][j] != response.World[i][j] {
						c.events <- CellFlipped{CompletedTurns: response.Turns, Cell: util.Cell{X: j, Y: i}}
					}
				}
			}
			previousWorld = copySlice(previousWorld)
			c.events <- TurnComplete{response.Turns}

		}
	}()
	exitSignal := make(chan bool)
	// Handle keypress events
	go func() {
		for {
			select {
			case key := <-c.key:
				switch key {
				case 's':
					request.S = true
					err := client.Call(BrokerKey, request, response)
					if err != nil {
						return
					}

					outputPGM(c, response.World, p, response.Turns)
				case 'q':
					c.events <- FinalTurnComplete{CompletedTurns: response.Turns, Alive: response.AliveCells}
					//check whether io is empty
					c.ioCommand <- ioCheckIdle
					<-c.ioIdle

					c.events <- StateChange{response.Turns, Quitting}

					close(exitSignal)
				case 'p':
					requestKey := Request{P: true}
					err := client.Call(BrokerKey, requestKey, response)
					if err != nil {
						return
					}
					if response.Turns%2 == 0 {
						fmt.Println("Paused at turn:", response.Turns)
						c.events <- StateChange{response.Turns, Paused}
					} else {
						fmt.Println("Continuing")
						c.events <- StateChange{response.Turns, Paused}
					}
				case 'k':
					wg.Add(1)
					request.K = true
					err := client.Call(BrokerKey, request, response)
					if err != nil {
						return
					}
					outputPGM(c, response.World, p, response.Turns)
					kill = true
					c.ioCommand <- ioCheckIdle
					<-c.ioIdle
					c.events <- StateChange{response.Turns, Quitting}
					wg.Done()
					exitSignal <- true
					os.Exit(0)
				}
			case <-exitSignal:
				return
			}
		}
	}()

	// Make the main RPC call to process all turns
	err = client.Call(BrokerKey, request, response)
	if err != nil {
		fmt.Printf("ProcessWorld error: %v\n", err)
		return
	}

	// Wait for completion
	wg.Wait()
	// Check if the specified last turn is finished
	if response.End {
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

		// Notify that image output is complete
		c.events <- ImageOutputComplete{
			CompletedTurns: p.Turns,
			Filename:       outputFilename,
		}

		// Ensure IO is complete
		c.ioCommand <- ioCheckIdle
		<-c.ioIdle

		c.events <- StateChange{response.Turns, Quitting}
		close(c.events)
	} else {
		// If not the last turn, just send the final turn complete event
		c.events <- FinalTurnComplete{
			CompletedTurns: response.Turns,
			Alive:          response.AliveCells,
		}

		// Ensure IO is complete
		c.ioCommand <- ioCheckIdle
		<-c.ioIdle

		c.events <- StateChange{response.Turns, Quitting}
		close(c.events)
	}
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
