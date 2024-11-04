package gol
import (
	"fmt"
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
func distributor(p Params, c distributorChannels) {
	// TODO: Create a 2D slice to store the world.
	world := make([][]byte, p.ImageHeight)
	for i := range world {
		world[i] = make([]byte, p.ImageWidth)
	}
	// TODO: Read the initial state from the io goroutine.
	c.ioCommand <- ioInput
	c.ioFilename <- fmt.Sprintf("%dx%d", p.ImageWidth, p.ImageHeight)
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			val := <-c.ioInput
			world[y][x] = val
			if val != 0 {
				c.events <- CellFlipped{CompletedTurns: 0, Cell: util.Cell{X: x, Y: y}}
			}
		}
	}
	turn := 0
	c.events <- StateChange{CompletedTurns: turn, NewState: Executing}
	// Create ticker for periodic reports
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	paused := false
	// Report initial alive cells
	c.events <- AliveCellsCount{CompletedTurns: turn, CellsCount: len(calculateAliveCells(p, world))}
	for turn < p.Turns {
		select {
		case <-ticker.C:
			//reports the number of alive cells in the game world during each turn of the Game of Life
			if !paused {
				c.events <- AliveCellsCount{CompletedTurns: turn, CellsCount: len(calculateAliveCells(p, world))}
			}
		case key := <-c.key:
			switch key {
			case 's':
				outputPGM(c, p, world, turn)
			case 'q':
				outputPGM(c, p, world, turn)
				c.events <- FinalTurnComplete{CompletedTurns: turn, Alive: calculateAliveCells(p, world)}
				c.events <- StateChange{CompletedTurns: turn, NewState: Quitting}
				return
			case 'p':
				paused = !paused
				if paused {
					c.events <- StateChange{CompletedTurns: turn, NewState: Paused}
				} else {
					c.events <- StateChange{CompletedTurns: turn, NewState: Executing}
				}
			}
		default:
			if !paused {
				newWorld := calculateNextState(p, world)
				flippedCells := []util.Cell{}
				// Collect all flipped cells
				for y := 0; y < p.ImageHeight; y++ {
					for x := 0; x < p.ImageWidth; x++ {
						if newWorld[y][x] != world[y][x] {
							flippedCells = append(flippedCells, util.Cell{X: x, Y: y})
						}
					}
				}
				// Send CellsFlipped event for all flipped cells
				if len(flippedCells) > 0 {
					c.events <- CellsFlipped{CompletedTurns: turn, Cells: flippedCells} // Use Cells instead of CellsToFlip
				}
				// Update the world to the new state
				world = newWorld
				turn++
				// Send TurnComplete event at the end of each turn
				c.events <- TurnComplete{CompletedTurns: turn}
			} else {
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
	// TODO: Report the final state using FinalTurnCompleteEvent.
	c.events <- FinalTurnComplete{
		CompletedTurns: turn,
		Alive:          calculateAliveCells(p, world),
	}
	// TODO: Output the final state as a PGM image.
	outputPGM(c, p, world, turn)
	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	c.events <- StateChange{CompletedTurns: turn, NewState: Quitting}
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
// outputPGM outputs the world state as a PGM image
func outputPGM(c distributorChannels, p Params, world [][]byte, turn int) {
	c.ioCommand <- ioOutput
	c.ioFilename <- fmt.Sprintf("%dx%dx%d", p.ImageWidth, p.ImageHeight, turn)
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			c.ioOutput <- world[y][x]
		}
	}
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	c.events <- ImageOutputComplete{CompletedTurns: turn, Filename: fmt.Sprintf("%dx%dx%d", p.ImageWidth, p.ImageHeight, turn)}
}
// calculateNextState computes the next state of the world using either single or multi-threaded approach
func calculateNextState(p Params, world [][]byte) [][]byte {
	newWorld := make([][]byte, p.ImageHeight)
	for i := range newWorld {
		newWorld[i] = make([]byte, p.ImageWidth)
	}
	// If only one thread is specified, use the single-threaded approach
	if p.Threads == 1 {
		return nextState(p, world, 0, p.ImageHeight)
	} else {
		// For multi-threaded approach, create channels for each thread
		chans := make([]chan [][]byte, p.Threads)
		// Divide the world into strips and process each strip in a separate goroutine
		for i := 0; i < p.Threads; i++ {
			chans[i] = make(chan [][]byte)
			a := i * (p.ImageHeight / p.Threads)
			b := (i + 1) * (p.ImageHeight / p.Threads)
			if i == p.Threads-1 {
				b = p.ImageHeight
			}
			// Create a copy of the world to avoid data races
			worldCopy := copySlice(world)
			// Start a worker goroutine for this strip
			go workers(p, worldCopy, chans[i], a, b)
		}
		// Collect results from all workers and combine them into the new world
		for i := 0; i < p.Threads; i++ {
			strip := <-chans[i]
			startRow := i * (p.ImageHeight / p.Threads)
			for r, row := range strip {
				newWorld[startRow+r] = row
			}
		}
	}
	return newWorld
}
func nextState(p Params, world [][]byte, start, end int) [][]byte {
	newWorld := make([][]byte, end-start)
	for i := range newWorld {
		newWorld[i] = make([]byte, p.ImageWidth)
	}
	directions := [8][2]int{
		{-1, -1}, {-1, 0}, {-1, 1},
		{0, -1}, {0, 1},
		{1, -1}, {1, 0}, {1, 1},
	}
	for row := start; row < end; row++ {
		for col := 0; col < p.ImageWidth; col++ {
			alive := 0
			for _, dir := range directions {
				newRow, newCol := (row+dir[0]+p.ImageHeight)%p.ImageHeight, (col+dir[1]+p.ImageWidth)%p.ImageWidth
				if world[newRow][newCol] == 255 {
					alive++
				}
			}
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
// workers processes a strip of the world and sends the result back through a channel
func workers(p Params, world [][]byte, result chan<- [][]byte, start, end int) {
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
func calculateAliveCells(p Params, world [][]byte) []util.Cell {
	var aliveCells []util.Cell
	for row := 0; row < p.ImageHeight; row++ {
		for col := 0; col < p.ImageWidth; col++ {
			if world[row][col] == 255 {
				aliveCells = append(aliveCells, util.Cell{X: col, Y: row})
			}
		}
	}
	return aliveCells
}
