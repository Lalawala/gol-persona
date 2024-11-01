package gol

import "uk.ac.bris.cs/gameoflife/util"

// Request represents the data sent to the GOL server
type Request struct {
	World     [][]byte // The current state of the world
	Parameter Params   // Game parameters including dimensions and turns
	P         bool     // For pause
	S         bool     // For save
	K         bool
}

type Response struct {
	World      [][]byte // The final state of the world
	Turns      int      // Number of completed turns
	Slice      [][]byte
	AliveCells []util.Cell // List of coordinates for alive cells
	CellCount  int
	End        bool
}
